# 03 — Command reference

Every command, every flag. Each entry shows the source file so you can jump
straight to the implementation.

## Global flags

Set on `forest` itself; available on all subcommands.

| Flag | Default | Meaning |
|------|---------|---------|
| `--project-root <path>` | "" (auto-detect) | Skip the upward walk for `.forest/`; treat `<path>` as the project root. |
| `--force` | false | Bypass safety checks: lets `task remove` delete dirty worktrees, lets `env sync` overwrite drifted destinations, lets `init` clobber an existing `.forest/`. |
| `-v`, `--verbose` | false | Currently registered but unused (see [`issues/correctness.md`](./issues/correctness.md)). |

Implementation: [`cmd/root.go`](../cmd/root.go).

---

## `forest init`

Bootstrap a forest project in the current directory.

```
forest init [--no-prompt] [--repos repo1,repo2,...]
```

| Flag | Meaning |
|------|---------|
| `--no-prompt` | Skip the multi-select; use detected repos. |
| `--repos a,b,c` | Use exactly this list (also implies no-prompt). |

Behaviour:

1. Refuses if `.forest/` already exists, unless `--force`.
2. Scans immediate subdirectories for `.git/`. With at least one TTY and no
   `--no-prompt` / `--repos`, asks which to track via a Huh multi-select.
   In non-interactive contexts (CI, agent shells), pass either `--no-prompt`
   (accepts all detected repos) or `--repos a,b,c` — otherwise the command
   fails with `--repos or --no-prompt is required in non-interactive mode`
   rather than trying to draw a prompt to a pipe.
3. Writes `.forest/config.yaml` with `default_base: main` for each chosen
   repo and the default agent / editor / worktrees-dir settings.
4. Writes `.forest/agents/00-overview.md` with a placeholder template.
5. Adds `.forest/worktrees` to each repo's `.git/info/exclude` so the
   worktree directory doesn't show up as untracked.

Source: [`cmd/init.go`](../cmd/init.go).

---

## `forest task create <name>`

Create a new task workspace.

```
forest task create <name>
    [--repos r1,r2]
    [--branch <name>]
    [--base  <name>]
    [--no-prompt]
```

Name rules: lowercase letters, digits, `.`, `_`, `-`, max 64 chars (regex
`^[a-z0-9][a-z0-9._-]{0,63}$`).

| Flag | Default | Meaning |
|------|---------|---------|
| `--repos` | (prompt) | Repo names from `config.yaml`. Required in non-TTY mode. |
| `--branch` | `task/<name>` | Branch name to create or check out. If it already exists in a repo, the existing branch is checked out into the new worktree. |
| `--base` | per-repo `default_base` | The base/start point for new branches. |
| `--no-prompt` | false | Fail rather than prompting for missing options. |

Effect: creates `.forest/worktrees/<name>/` containing:
- one git worktree per chosen repo,
- `.forest-task.yaml` metadata,
- `AGENTS.md` and `CLAUDE.md`,
- copies of every file declared in `env_files`.

If any worktree creation fails partway through, all already-created worktrees
and the task directory are rolled back.

Source: [`cmd/task_create.go`](../cmd/task_create.go) → [`internal/workspace/Create`](../internal/workspace/workspace.go).

---

## `forest task list` (alias `ls`)

List all task workspaces.

```
forest task list [--json]
```

Plain output is a tab-aligned table: name, repos, branches, dirty flag,
created date. With `--json` you get an array of `listEntry` objects with the
same fields plus the absolute task `dir`.

Source: [`cmd/task_list.go`](../cmd/task_list.go).

---

## `forest task status [<name>]`

Per-repo state summary. With no arg, every task.

Columns: `TASK`, `REPO`, `BRANCH`, `AHEAD/BEHIND` (vs base), `DIRTY` (count
breakdown like `M3 S1 U2` if not clean), `LAST` (last commit subject,
truncated to 50 chars).

Source: [`cmd/task_status.go`](../cmd/task_status.go).

---

## `forest task remove <name>` (aliases `rm`, `delete`)

Tear down a task.

```
forest task remove <name> [--keep-branches]
```

| Flag | Meaning |
|------|---------|
| `--keep-branches` | Don't delete the task branches in each repo after removing the worktrees. |
| (global) `--force` | Skip the dirty/unpushed safety checks; pass `-D`/`--force` to git so it removes worktrees and branches even when dirty/unmerged. |

Order of operations: safety check (unless `--force`), `git worktree remove`
each repo, `git branch -d` (or `-D` with `--force`) unless
`--keep-branches`, `git worktree prune`, `os.RemoveAll(taskDir)`.

Source: [`cmd/task_remove.go`](../cmd/task_remove.go) → [`internal/workspace/Remove`](../internal/workspace/workspace.go).

---

## `forest agent start [<task>]`

`chdir` to the task directory and `exec` the configured AI agent (process
replacement; the forest process disappears).

```
forest agent start [<task>] [--agent <name>]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--agent <name>` | `agents.default` | A key from `agents.options` in `config.yaml`. |

If the task name is omitted in a TTY, you get a Huh single-select. In
non-TTY mode the task name is required.

Source: [`cmd/agent_start.go`](../cmd/agent_start.go) → [`internal/runner/ExecReplace`](../internal/runner/runner.go).

---

## `forest editor open [<task>]`

Spawn (not exec) the configured editor in the task directory. Most GUI
editors (cursor, code) fork themselves and return immediately, leaving you
back at your shell.

```
forest editor open [<task>] [--editor <name>]
```

Source: [`cmd/editor_open.go`](../cmd/editor_open.go) → [`internal/runner/Spawn`](../internal/runner/runner.go).

---

## `forest env sync [<task>]`

Copy every file in `config.env_files` from the project root into the named
task workspace (or all tasks if omitted).

States returned per file (printed to stdout):

| State | Meaning |
|-------|---------|
| `copied` | Source written to dest. |
| `skipped-identical` | Dest already matches source — no-op. |
| `skipped-missing-source` | Source file doesn't exist; dest left alone. |
| `refused-modified` | Dest exists and differs from source; left alone. Re-run with `--force` to overwrite. |

Exits non-zero if any file ended up `refused-modified` (and `--force` was not
passed).

Source: [`cmd/env_sync.go`](../cmd/env_sync.go) → [`internal/envfiles/Sync`](../internal/envfiles/envfiles.go).

---

## `forest env diff [<task>]`

Read-only counterpart to `env sync`. Reports `match` / `drift` /
`missing-source` / `missing-dest` per file, prints a unified diff for any
drift, and exits with code 2 if any drift was found.

Source: [`cmd/env_diff.go`](../cmd/env_diff.go) → [`internal/envfiles/Diff`](../internal/envfiles/envfiles.go).

---

## `forest config edit agents`

Open `.forest/agents/` in `$EDITOR` (or `$VISUAL`, or `vi`); on exit,
regenerate `AGENTS.md` and `CLAUDE.md` for **every** task workspace.

This is the supported way to update the project-wide context that gets
concatenated into every task's agent file.

Source: [`cmd/config_edit.go`](../cmd/config_edit.go).

(There's a known shell-injection issue here when `$EDITOR` is hostile; see
[`issues/security.md`](./issues/security.md).)

---

## `forest tui`

Launches the Bubble Tea two-pane explorer. See
[`05-bubble-tea-tui.md`](./05-bubble-tea-tui.md).

| Key | Action |
|-----|--------|
| `j` / `↓` | Move cursor down |
| `k` / `↑` | Move cursor up |
| `r` | Reload tasks |
| `s` | Env-sync the highlighted task (no `--force`) |
| `d` | Remove the highlighted task (refuses on dirty/unpushed) |
| `q` / `Esc` / `Ctrl+C` | Quit |

Source: [`cmd/tui.go`](../cmd/tui.go) → [`internal/tui/`](../internal/tui/).

---

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success. |
| 1 | Most failures (the message is printed to stderr by `cmd.Execute`). |
| 2 | `forest env diff` found drift. |
