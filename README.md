# forest

`forest` manages **per-task git worktree workspaces across multiple repos**.

If you work on a project made of several repositories (say a `frontend`, a
`backend`, and a `shared` library) and you want to start a piece of work that
touches more than one of them, `forest` gives you a single directory holding a
fresh git worktree of each repo, all on a shared task branch, plus the env files
and AI-agent context that workspace needs. When the work is done, one command
tears the whole thing back down — refusing, by default, to throw away anything
you haven't committed and pushed.

```
.forest/worktrees/my-task/
├── frontend/        # git worktree of frontend on branch task/my-task
├── backend/         # git worktree of backend  on branch task/my-task
├── AGENTS.md        # generated context (concatenated from .forest/agents/*.md)
├── CLAUDE.md        # identical copy of AGENTS.md
└── .forest-task.yaml  # task metadata
```

## Why worktrees?

A git *worktree* lets one repository have several working directories checked
out at once, each on its own branch, sharing a single `.git`. That means
switching tasks doesn't mean stashing, and two tasks can't clobber each other's
working tree. `forest` coordinates one worktree per repo per task so a
multi-repo change lives in one place.

## Install

```sh
go install github.com/JonnyTizz/forest@latest   # installs the `forest` binary
# or, from a clone:
go build -o forest . && mv forest /somewhere/on/your/PATH
```

> **Platform note:** `forest agent start` uses `syscall.Exec` to replace the
> process with your agent, which is POSIX-only. The rest of the tool is
> cross-platform, but agent launching is unsupported on Windows.

## Quickstart

From the directory that contains your repos as immediate subdirectories:

```sh
forest init                       # detect repos, write .forest/config.yaml
forest task create login-flow     # pick repos interactively, make the workspace
cd "$(forest task path login-flow)"
forest agent start login-flow     # launch your configured AI agent in the workspace
# ... do the work, commit, push ...
forest task status                # review branch/dirty/ahead-behind state
forest task remove login-flow     # tear it down (refuses if dirty/unpushed)
```

## Concepts

| Term | Meaning |
|------|---------|
| **Project root** | The directory containing `.forest/`. Found by walking up from the CWD, or set with `--project-root`. |
| **Task** | A named workspace under `.forest/worktrees/<name>/` holding one worktree per selected repo. |
| **Task branch** | The branch all of a task's worktrees check out. Defaults to `task/<name>`. |
| **Base** | The branch a new task branch forks from. Per repo, defaults to that repo's `default_base`. |
| **Agents fragments** | The `*.md` files in `.forest/agents/`, concatenated in filename order into each task's `AGENTS.md`/`CLAUDE.md`. |

## Command reference

### `forest init`
Initialise a project in the current directory. Detects git repos in immediate
subdirectories and writes `.forest/config.yaml` plus a starter
`.forest/agents/00-overview.md`. The `default_base` for each repo is detected
from its actual default branch (so `master` repos work), falling back to `main`.

- `--repos a,b` — use exactly these repos instead of prompting.
- `--no-prompt` — accept detected defaults without the interactive picker.
- `--force` — overwrite an existing `.forest/` config.

### `forest task create <name>`
Create a task workspace.

- `--repos a,b` — repos to include (required in non-interactive mode; otherwise prompts).
- `--branch <name>` — branch to create or, if it already exists, check out (default `task/<name>`).
- `--base <branch>` — base to fork from (default: each repo's `default_base`).
- `--no-prompt` — fail instead of prompting for missing options.

If the branch already exists in a repo, `forest` checks out that existing branch
rather than creating a new one. If creation fails partway, every worktree it
already made is rolled back and the task directory is removed; a branch that
already existed is left alone.

### `forest task list` (alias `ls`)
List tasks with their repos, branches, dirty flag, and creation date.
`--json` emits machine-readable output.

### `forest task status [<name>]`
Per-repo branch, ahead/behind vs base, dirty counts (`M`odified / `S`taged /
`U`ntracked), and last commit subject. With no name, shows every task.

### `forest task path <name> [<repo>]`
Print the absolute path of the task workspace (or one repo's worktree within
it), for use with `cd "$(forest task path my-task)"`.

### `forest task remove <name>` (aliases `rm`, `delete`)
Remove a task. **Refuses by default** if any worktree is dirty or has commits
not present on a remote. On refusal nothing is deleted.

- `--keep-branches` — remove worktrees but keep the task branches.
- `--force` — bypass the dirty/unpushed checks and force worktree removal.

### `forest agent start [<task>] [--agent <name>]`
`chdir` into the task workspace and exec the configured AI agent (replacing the
`forest` process). Prompts for the task if omitted and stdin is a TTY.

### `forest editor open [<task>] [--editor <name>]`
Launch the configured editor (e.g. `cursor`, `code`) in the task workspace.

### `forest env sync [<task>]`
Copy configured env files from the project root into the task(s). A destination
that exists and differs is **refused** (reported, not overwritten) unless
`--force` is given. With no task name, syncs every task.

### `forest env diff [<task>]`
Show drift between project-root env files and their copies in the task(s).
Exits **2** if any file has drifted (distinct from exit 1 for errors), so it
slots into scripts and pre-commit checks.

### `forest config edit agents`
Open `.forest/agents/` in `$EDITOR`; on exit, regenerate `AGENTS.md`/`CLAUDE.md`
for every task so context edits propagate.

### `forest config edit config`
Open `.forest/config.yaml` in `$EDITOR`; on exit, re-validate it and report any
problems (the file is left in place so you can fix it).

### `forest doctor [--fix]`
Report inconsistent state: orphaned task directories (no metadata, e.g. from a
crashed `create`) and stale git worktree entries pointing at task workspaces.
Exits 2 when problems are found. `--fix` removes orphan directories and prunes
stale worktree entries from each repo.

### `forest tui`
A read-only interactive task explorer (navigate with `j`/`k`, `r` reload,
`s` env-sync, `d` remove-with-confirmation, `q` quit). Task creation and agent
launching are CLI-only.

### Global flags
- `--project-root <dir>` — use this project root instead of walking up from CWD.
- `--force` — bypass safety checks (meaning is per-command, as described above).
- `-v, --verbose` — verbose output.

## Configuration (`.forest/config.yaml`)

```yaml
version: 1
worktrees_dir: .forest/worktrees    # where task workspaces live (relative to root or absolute)

repos:
  - name: frontend                  # used as the worktree subdir name; no slashes/spaces
    path: frontend                  # path to the repo, relative to the project root (or absolute)
    default_base: main              # base branch new task branches fork from

env_files:                          # optional; copied into each task workspace
  - source: .env                    # relative to project root; must stay within it
    dest: frontend/.env             # relative to the task dir; must stay within it

agents:                             # named commands for `forest agent start`
  default: claude
  options:
    claude: { command: claude }
    aider:  { command: aider }

editors:                            # named commands for `forest editor open`
  default: cursor
  options:
    cursor: { command: cursor, args: ["."] }
    code:   { command: code,   args: ["."] }
```

Notes:
- `version` must be `1`.
- Repo `name`s must be unique and contain no slashes or spaces.
- `env_files` `source`/`dest` are rejected if absolute or if they escape their
  root (e.g. `../../.ssh/id_rsa`). This protects you from a config you didn't
  write yourself.
- `agents.default` / `editors.default` must name an entry in their `options`.

## Edge cases & behaviour worth knowing

- **Existing branches.** `task create` checks out an existing task branch
  instead of creating a new one. The base is then irrelevant for that repo.
- **Removal safety.** `task remove` refuses on a dirty or unpushed worktree and
  deletes nothing. If a worktree can't be removed and you didn't pass `--force`,
  the task directory is left intact rather than orphaning git's worktree
  metadata — run `forest doctor` to see and `--fix` to clean up.
- **"Unpushed" is scoped to the task branch (HEAD).** A local `main` that is
  ahead of `origin/main` does *not* block removing an unrelated task.
- **Repos with no remote.** The unpushed check is skipped (there's nothing to
  push to), so removal is gated only by the dirty check.
- **Stale base.** Worktrees fork from your *local* base ref; `forest` does not
  fetch first. If your local `main` is behind, run `git fetch` in the repo
  before `task create` (or pass an up-to-date `--base`).
- **Env drift workflow.** Edit env at the project root, then `forest env sync`
  to push it into tasks; `forest env diff` (exit 2 on drift) to detect copies
  that diverged.

## Troubleshooting

- **"task already exists" but it's not in `task list`.** A `create` failed after
  making the directory but before writing metadata. `forest doctor --fix`
  removes the orphan.
- **Stale worktree warnings / "worktree already exists".** Run
  `forest doctor --fix` to prune git's leftover worktree admin entries.
- **`task remove` says "not fully removed".** A worktree couldn't be removed
  cleanly. Investigate it, then rerun with `--force`.

See [`documents/architecture.md`](documents/architecture.md) for an explainer of
how the code is structured, and
[`documents/issues/`](documents/issues/) for the security audit and code review.
