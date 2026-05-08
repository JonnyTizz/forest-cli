# 04 — Code tour

A package-by-package walk through the source so you can confidently change
it. The order below is the order you should read the files in.

```
forest/
├── main.go
├── cmd/                      ← all Cobra command definitions
│   ├── root.go
│   ├── init.go
│   ├── task.go, task_create.go, task_list.go, task_status.go, task_remove.go
│   ├── agent.go, agent_start.go
│   ├── editor.go, editor_open.go
│   ├── env.go, env_sync.go, env_diff.go
│   ├── config.go, config_edit.go
│   └── tui.go
└── internal/                 ← business logic, no Cobra here
    ├── config/    config schema (YAML), Load/Save/Validate
    ├── project/   project-root discovery, Project struct
    ├── gitx/      thin wrapper around `git` CLI
    ├── workspace/ task lifecycle (create / remove / list / regen agents)
    ├── envfiles/  env file Sync + Diff
    ├── agents/    render AGENTS.md / CLAUDE.md from .forest/agents/*.md
    ├── safety/    DirtyError / UnpushedError, EnsureClean / EnsureNoUnpushed
    ├── runner/    Resolve a CommandSet entry, ExecReplace / Spawn
    └── tui/       Bubble Tea model, view, styles
```

Two clean layers: `cmd/` is presentation (Cobra wires CLI flags, prints
results, prompts via Huh), `internal/` is logic (no IO formatting, returns
typed errors and structs). That separation is what makes the code easy to
reason about and easy to test (though there are no tests yet — see
[`issues/correctness.md`](./issues/correctness.md)).

---

## `main.go` — the entry point

```go
package main

import "github.com/JonnyTizz/forest/cmd"

func main() { cmd.Execute() }
```

Three lines. Every command lives under `cmd/` and registers itself with the
root command via package-level `init()` functions. `cmd.Execute()` runs the
root, which Cobra dispatches based on argv.

## `cmd/root.go` — the root command and global flags

The `rootCmd` defines `forest` itself plus three persistent flags
(`--project-root`, `--force`, `-v/--verbose`). Each `cmd/*.go` file declares
its own `cobra.Command` and calls `rootCmd.AddCommand` (or a sub-group's
`AddCommand`) inside its `init()`.

`SilenceUsage`/`SilenceErrors` are both true: errors print as `error: ...`
followed by exit 1, without Cobra's default usage spam on every failure.

`Force()`, `Verbose()`, `ProjectRootFlag()` are exported accessors so tests
or future packages can read the flag values without touching the package
internals. (They're not currently used outside the package.)

## `cmd/<sub>.go` files — one Cobra group, plural verb files

Each grouping has a "container" command (e.g. `cmd/task.go` defines
`taskCmd`) plus per-verb files (`task_create.go`, `task_list.go`, …). The
verb file:

1. Declares any flags with package-level vars (`tcRepos`, `tcBranch`, …).
2. Wires the cobra command in `init()` and calls `<group>Cmd.AddCommand`.
3. Has a `runFoo` function that does the work — typically:
   - load the project (`project.Load`),
   - resolve any prompt-or-flag inputs,
   - call into `internal/...`,
   - format the result.

This pattern is consistent everywhere; once you've read `task_create.go` you
can read any other one in 30 seconds.

---

## `internal/config` — the YAML schema and validation

`config.go` defines the on-disk schema (`ProjectConfig`), defaults
(`Defaults()`), and `Load`/`Save`/`Validate`. Constants you'll see used:

- `Dirname = ".forest"`
- `ConfigFile = "config.yaml"`
- `AgentsDir = "agents"`
- `DefaultWorktreesSubdir = "worktrees"`

Validation enforces:

- `version: 1` only,
- at least one repo,
- repo names: non-empty, unique, no slashes/spaces,
- repo paths: non-empty (defaults `default_base` to `main` if missing),
- `agents.default` and `editors.default`, if set, must reference a key in
  their respective `options` map.

Note what is **not** validated: `Repo.Path` is not constrained to be inside
the project root, and absolute paths are accepted. This is one of the
findings in [`issues/security.md`](./issues/security.md).

`FindRepo` / `RepoNames` / `SortedRepoNames` are convenience accessors used
across `cmd/`.

## `internal/project` — turning a CWD into a `Project`

`FindRoot(override)` walks up from the current directory until it finds a
`.forest/` directory. Almost every command starts with `project.Load(...)`
which combines `FindRoot` + `config.Load`.

The `Project` struct has helper methods that resolve paths:

| Method | Returns |
|--------|---------|
| `ForestDir()` | `<root>/.forest` |
| `AgentsDir()` | `<root>/.forest/agents` |
| `WorktreesDir()` | absolute path of `config.WorktreesDir` |
| `TaskDir(name)` | `<worktrees-dir>/<name>` |
| `RepoPath(name)` | absolute path of the configured repo |

This is the only place that knows how config paths translate to filesystem
paths. Keeping that logic here is what lets the rest of the code take a
`*project.Project` and not worry about whether `worktrees_dir` is relative or
absolute.

## `internal/gitx` — a typed git CLI wrapper

Forest doesn't use a Go git library; it shells out to `git`. Every helper in
`gitx.go` is a thin wrapper around `exec.Command("git", ...)` that returns
typed Go values. Read the file top-to-bottom — it's 165 lines and you'll see
patterns repeated:

- `Run(dir, args...)` runs `git` from `dir`, returns trimmed stdout, wraps
  stderr into the error.
- `IsRepo`, `BranchExists`, `CurrentBranch` — predicates / scalars.
- `WorktreeAdd`, `WorktreeRemove`, `WorktreePrune`, `BranchDelete` —
  mutators.
- `Stat(dir)` parses `git status --porcelain=v1 --untracked-files=normal`
  into a `Status` struct (modified/staged/untracked counts, branch, last
  commit subject).
- `AheadBehind(dir, base)` parses `git rev-list --left-right --count
  HEAD...base`.
- `HasUnpushed(dir)` returns true if there are commits on local branches not
  on any remote.

Because everything goes through `Run`, error messages always include the
exact `git` invocation that failed plus stderr — handy when debugging.

There's a subtle bug in `WorktreeAdd` when reusing an existing branch; see
[`issues/correctness.md`](./issues/correctness.md#1).

## `internal/safety` — refusal errors

Two error types you'll see in flag-gated paths (`task remove` without
`--force`):

- `*DirtyError` — wraps a `gitx.Status`, formatted as `worktree X is dirty
  (3 modified, 1 staged, 2 untracked)`.
- `*UnpushedError` — formatted as `worktree X has unpushed commits`.

`EnsureClean` and `EnsureNoUnpushed` take a worktree path, run `gitx.Stat`/
`gitx.HasUnpushed` and return a typed error if applicable.

The point of separate types is so callers (or callers' callers, or a future
TUI) can distinguish "user did something wrong" from "real I/O failure". In
practice they're only used by `workspace.Remove`.

## `internal/agents` — rendering CLAUDE.md / AGENTS.md

`Render(agentsDir, meta)` reads every `*.md` file under `agentsDir`, sorts
by filename (so `00-`, `10-`, `20-` ordering works), and concatenates them
under a generated header that lists the task's repos.

`WriteAll(taskDir, content)` writes the result to both `AGENTS.md` and
`CLAUDE.md`. Two filenames because Claude Code looks for `CLAUDE.md` and
some other agents look for `AGENTS.md`; the contents are identical.

This package has no business logic — it's pure rendering. That's why
`workspace.RegenerateAgents` and `workspace.RegenerateAllAgents` can be
trivial wrappers.

## `internal/envfiles` — sync and diff

`Sync(projectRoot, taskDir, files, force) []Result`:

For each `EnvFile{Source, Dest}`:
1. Reads source bytes; if missing, returns `skipped-missing-source`.
2. If dest exists and matches → `skipped-identical`.
3. If dest exists and differs and `!force` → `refused-modified`.
4. Else writes dest → `copied`.

`Diff` is the read-only counterpart, returning `DriftEntry` with a unified
line diff for drift cases.

`unifiedLineDiff` is a homegrown minimal diff (line-by-line, not LCS). Fine
for env files — they're shallow key=value lists where reordering rarely
matters.

`absUnder(root, p)` resolves relative paths under `root` and lets absolute
paths through unchanged. The unchecked absolute-path path is the second
critical issue in [`issues/security.md`](./issues/security.md).

## `internal/runner` — running external commands

Two important entry points:

- `ExecReplace(cwd, spec, extraArgs)` — looks up the binary on `PATH`,
  `os.Chdir`s, `syscall.Exec`s. Used for **agents**: process replacement
  means signals/TTY pass through cleanly and the forest process disappears
  while the agent runs.
- `Spawn(cwd, spec, extraArgs)` — `exec.Command` + `Run` with stdio wired
  up. Used for **editors**: GUI editors typically fork and detach, so this
  returns quickly.

`Resolve(set, name)` looks up a name in a `CommandSet` (with empty-name →
default), returning `(spec, effectiveName, err)`.

`EditorCommand()` returns `$EDITOR` else `$VISUAL` else `vi`. Used by
`forest config edit agents`.

## `internal/workspace` — the heart of the tool

This is where the orchestration lives. Read it carefully.

- `ValidateName(name)` — task-name regex check.
- `Create(p, opts) (*Task, error)` — top-level `task create`. Builds the
  task dir, creates a worktree per repo, writes meta, regenerates agent
  files, syncs env files. On any error mid-way, calls `rollback` to undo the
  worktrees and removes the task dir.
- `Remove(p, name, force, keepBranches)` — opposite of `Create`, with
  safety checks gated on `force`. Errors during git removal are warned to
  stderr but don't abort the cleanup of remaining repos — better to leave
  the user in a partially-clean state than to give up halfway.
- `Get(p, name)` — load `.forest-task.yaml` for a single task.
- `List(p)` — load every task in the worktrees dir (sorted by name).
- `RegenerateAgents` / `RegenerateAllAgents` — write CLAUDE.md/AGENTS.md.
- `SuggestRepoOrder` — repo names that exist on disk, in config order.
- `IgnoreInExclude(repoDir, line)` — append to `.git/info/exclude`,
  idempotent. Called by `init`.

`Meta` and `RepoMeta` define the on-disk task metadata. The `Task` struct
hydrates that with the absolute task directory.

## `internal/tui` — the Bubble Tea UI

Two files:

- `app.go` — the `model` (state), `Update` (input handling), `View`
  (rendering).
- `styles.go` — Lipgloss styles used by the view.

Covered in detail in [`05-bubble-tea-tui.md`](./05-bubble-tea-tui.md).

---

## How a single command flows end-to-end

Take `forest task create add-billing --repos api,web`:

1. **Cobra** parses argv, populates `tcRepos = ["api", "web"]`, calls
   `runTaskCreate` with `args = ["add-billing"]`.
2. `runTaskCreate` validates the name ([`workspace.ValidateName`](../internal/workspace/workspace.go))
   and loads the project ([`project.Load`](../internal/project/project.go)).
3. With `--repos` set, no Huh prompt is shown. (Otherwise we'd build
   `[]huh.Option` from `workspace.SuggestRepoOrder` and run a multiselect.)
4. `runTaskCreate` calls [`workspace.Create`](../internal/workspace/workspace.go).
5. `workspace.Create` builds `<root>/.forest/worktrees/add-billing/`, then
   for each repo:
   - resolves the repo path via `project.RepoPath`,
   - decides whether the branch already exists with `gitx.BranchExists`,
   - calls `gitx.WorktreeAdd` to create the worktree.
   - On error, calls `rollback` to remove already-created worktrees and
     `os.RemoveAll(taskDir)`.
6. Writes `.forest-task.yaml` (`writeMeta`), regenerates `AGENTS.md` /
   `CLAUDE.md` (`RegenerateAgents` → `agents.Render` + `agents.WriteAll`),
   syncs env files (`envfiles.Sync`).
7. Returns the `*Task`. `runTaskCreate` prints a friendly summary and
   returns nil. Cobra exits 0.

Every command follows this shape: parse, load project, resolve inputs, call
into a single `internal/` function, format the result.

## How to add a new command

If you wanted to add `forest task open <name>` that just printed the task
directory:

1. Create `cmd/task_open.go`:
   ```go
   package cmd

   import (
       "fmt"
       "github.com/spf13/cobra"
       "github.com/JonnyTizz/forest/internal/project"
       "github.com/JonnyTizz/forest/internal/workspace"
   )

   var taskOpenCmd = &cobra.Command{
       Use:   "open <name>",
       Short: "Print the task workspace path",
       Args:  cobra.ExactArgs(1),
       RunE: func(cmd *cobra.Command, args []string) error {
           p, err := project.Load(flagProjectRoot)
           if err != nil { return err }
           t, err := workspace.Get(p, args[0])
           if err != nil { return err }
           fmt.Println(t.Dir)
           return nil
       },
   }

   func init() { taskCmd.AddCommand(taskOpenCmd) }
   ```
2. That's it — the file is picked up by `package cmd`'s `init()` chain
   automatically.

If the command needs new behaviour (not just a thin wrapper around an
existing `internal/` function), add the logic to `internal/` first, keep
`cmd/` thin.

## Where to look for what

| If you want to change… | Edit… |
|------------------------|-------|
| What `forest init` writes to config.yaml | `internal/config.Defaults` |
| The set of git operations | `internal/gitx` |
| What CLAUDE.md / AGENTS.md look like | `internal/agents/render.go` |
| When env file sync refuses overwrites | `internal/envfiles.Sync` |
| Safety checks before remove | `internal/safety` and `internal/workspace.Remove` |
| TUI key bindings | `internal/tui/app.go` |
| TUI colours / layout | `internal/tui/styles.go` |
| CLI flag names / shapes | the relevant `cmd/<verb>.go` |
