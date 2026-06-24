# forest architecture

This document explains how `forest` is put together so you can read the code
with a map in hand. It reflects the code as it actually is, not an aspiration.

## Shape of the program

`forest` is a [Cobra](https://github.com/spf13/cobra) CLI. `main.go` calls
`cmd.Execute()`; everything user-facing is a Cobra command under `cmd/`, and all
the real logic lives in small single-purpose packages under `internal/`.

```
main.go
└── cmd/                # Cobra command definitions (thin; parse flags, call internal/)
    ├── root.go         # root command, global flags, ExitCodeError plumbing
    ├── init.go         # `forest init`
    ├── task*.go        # task create/list/status/remove/path
    ├── agent*.go       # `forest agent start`
    ├── editor*.go      # `forest editor open`
    ├── env_*.go        # `forest env sync|diff`
    ├── config_edit.go  # `forest config edit agents|config`
    ├── doctor.go       # `forest doctor`
    └── tui.go          # `forest tui`
└── internal/
    ├── config/         # config.yaml schema, load/save, validation
    ├── project/        # locate the project root, resolve paths
    ├── gitx/           # thin wrapper over the `git` binary
    ├── workspace/      # the core: task lifecycle (create/remove/list)
    ├── envfiles/       # copy/diff env files into task workspaces
    ├── agents/         # render AGENTS.md/CLAUDE.md from fragments
    ├── runner/         # exec/spawn agents & editors, split editor cmd
    ├── safety/         # "is this worktree safe to delete?" checks
    └── tui/            # Bubble Tea read-only task explorer
```

The dependency direction is one-way: `cmd` depends on `internal`; within
`internal`, `workspace` is the hub that pulls in `gitx`, `envfiles`, `agents`,
`safety`, and `project`. Nothing in `internal` imports `cmd`.

## Key types

- **`config.ProjectConfig`** — the parsed `.forest/config.yaml`: the list of
  `Repo`s (name, path, default base), optional `EnvFile`s, and the `agents` /
  `editors` `CommandSet`s. `Validate()` enforces the invariants (version is 1,
  repo names unique and slash-free, defaults named in their options).
- **`project.Project`** — a resolved root plus its loaded config. It owns all
  path math: `WorktreesDir()`, `TaskDir(name)`, `RepoPath(name)`, `AgentsDir()`.
- **`workspace.Meta`** — what's persisted as `<task>/.forest-task.yaml`: the
  task name, creation time, and a `RepoMeta` (name/branch/base) per repo.
- **`workspace.Task`** — a hydrated `Meta` plus its on-disk `Dir`.

## How the project root is found

`project.FindRoot` walks up from the current directory looking for a `.forest/`
directory (or uses `--project-root` verbatim). `project.Load` then reads and
validates `.forest/config.yaml`. Every command starts with `project.Load`, so
the whole tool is implicitly scoped to "the nearest forest project above me".

## The task lifecycle

### Create — `workspace.Create` (the most important function to understand)

1. Validate the task name and the branch/base refs.
2. Refuse if the task directory already exists.
3. For each selected repo, in config order:
   - resolve the repo path and effective base;
   - decide new-branch vs check-out-existing by asking
     `gitx.BranchExists`;
   - `gitx.WorktreeAdd` creates the worktree;
   - record a `createdWorktree{repoPath, path, branch, branchByThis}` for
     rollback, where `branchByThis` is true only when *we* created the branch.
4. Write `.forest-task.yaml`, render the agent files, sync env files.

Every failure after the first worktree exists calls `rollback`, which removes
the worktrees it made and deletes **only the branches it created** (a
pre-existing branch that was merely checked out is preserved), then removes the
task directory. This is what keeps a half-finished `create` from leaving a
wedged workspace behind.

### Remove — `workspace.Remove`

1. Load the task.
2. Unless `--force`: run the **safety pass** — `safety.EnsureClean` (no
   modified/staged/untracked files) and `safety.EnsureNoUnpushed` (HEAD has no
   commits absent from every remote) on each worktree. Any failure aborts before
   anything is deleted.
3. Remove each worktree and (unless `--keep-branches`) delete its branch,
   collecting non-fatal problems as `warnings` (returned to the caller rather
   than printed, so the TUI's alt-screen isn't corrupted).
4. If a worktree couldn't be removed and we're not forcing, stop *before*
   deleting the task directory — leaving things consistent for `forest doctor`.
   Otherwise remove the directory and prune git's worktree admin entries.

### List / status

`workspace.List` reads every subdirectory of the worktrees dir that has a
readable `.forest-task.yaml`. `forest task status` layers live `gitx.Stat` and
`gitx.AheadBehind` data on top.

## The `git` boundary — `internal/gitx`

`gitx` never imports a git library; it shells out to the `git` binary via
`gitx.Run`, capturing stdout and wrapping stderr on failure. It's deliberately
thin and stateless. The functions worth knowing:

- `WorktreeAdd(repo, path, newBranch, checkout, base)` — exactly one of
  `newBranch` (create `-b`) or `checkout` (existing branch) is set.
- `Stat` — parses `git status --porcelain=v1` into modified/staged/untracked
  counts plus branch and last-commit subject.
- `HasUnpushed` — commits reachable from **HEAD** but no remote ref (scoped to
  the current branch, so unrelated local branches don't trigger it).
- `DefaultBranch` — `origin/HEAD` → current branch → `main`.
- `Worktrees` — parses `git worktree list --porcelain` (used by `doctor`).

Branch/base names are validated by `workspace.ValidateRef` before reaching git,
so values that look like options (`-f`) or contain git-special characters are
rejected rather than passed through.

## Env files — `internal/envfiles`

`Sync` copies each configured file from the project root into the task dir,
reporting one of `copied` / `skipped-identical` / `skipped-missing-source` /
`refused-modified` per file (it never overwrites a diverged copy without
`--force`). `Diff` reports `match` / `drift` / `missing-source` / `missing-dest`
with a minimal line diff. Both resolve paths through `containedUnder`, which
rejects absolute paths and `..` escapes so a config can't read or write outside
its root.

## Agent context — `internal/agents`

`Render` concatenates every `*.md` in `.forest/agents/` (sorted by filename, so
numeric prefixes order them) under a generated header that lists the task's
repos/branches. `WriteAll` writes the result to both `AGENTS.md` and `CLAUDE.md`
in the task dir. `forest config edit agents` re-runs this for every task after
you edit the fragments.

## Running agents & editors — `internal/runner`

- `ExecReplace` (agents): `syscall.Exec` replaces the `forest` process so
  signals and the TTY pass straight through to the agent. POSIX-only.
- `Spawn` (editors): a normal child process; editors that fork-and-detach
  (`cursor`, `code`) return promptly.
- `SplitArgs` parses an `$EDITOR` string into argv with quote/escape handling,
  so `forest config edit` can run it **without** a shell — closing the
  shell-injection hole the old `sh -c` approach had.

## Exit codes

`cmd.ExitCodeError{Code, Err}` lets a command request a specific process exit
status. `forest env diff` and `forest doctor` use it to exit `2` ("drift /
problems found") as distinct from `1` ("error"), which scripts can branch on.

## What the TUI is (and isn't)

`internal/tui` is a Bubble Tea app that lists tasks and shows per-repo status.
It is intentionally read-mostly: it can env-sync (`s`) and remove a task (`d`,
with a `y/N` confirmation), but it cannot create tasks or launch agents — those
stay in the CLI. Treat it as a dashboard, not the primary interface.
