# 01 — What forest is and why it exists

## The problem

You have a "multi-repo application" — backend, frontend, infra, shared libs,
maybe a docs site — each in its own git repo, all checked out as siblings under
one project directory:

```
~/work/myapp/
├── backend/      ← git repo
├── frontend/     ← git repo
├── infra/        ← git repo
└── shared-lib/   ← git repo
```

When you want to make a single logical change that touches more than one of
those repos (a feature, a refactor, a coordinated API rename), you'd normally
have to:

1. Create the same feature branch in each repo (`git checkout -b foo` × N).
2. Remember which repos you've changed and which you haven't.
3. Switch branches in all of them when the agent or human jumps to a different
   piece of work — losing your in-progress changes or stashing them.
4. Hand an AI agent enough context that it knows which repos exist, which
   branch is "current," and how they relate.

`forest` collapses that into one command: "create a task workspace named
`add-billing` covering these repos." It uses **git worktrees** so each repo
keeps its main checkout untouched — you can simultaneously have `main` checked
out in the original `backend/` and a `task/add-billing` checkout under the
forest task directory.

## The mental model

A **forest project** is the top-level directory that contains your repos and
a `.forest/` config directory.

A **task** is a self-contained workspace: one directory containing a git
worktree of each chosen repo, all on the same branch name (`task/<name>` by
default), plus generated agent-context files (`CLAUDE.md`, `AGENTS.md`) and
optional copies of env files.

```
~/work/myapp/                      ← project root
├── backend/                       ← original checkout (e.g. on main)
├── frontend/                      ← original checkout (e.g. on main)
├── .forest/
│   ├── config.yaml                ← which repos, which agents/editors
│   ├── agents/                    ← markdown fragments concatenated into
│   │   ├── 00-overview.md           every task's CLAUDE.md
│   │   ├── 10-frontend.md
│   │   └── 20-backend.md
│   └── worktrees/
│       ├── add-billing/           ← TASK WORKSPACE
│       │   ├── backend/             worktree of backend on task/add-billing
│       │   ├── frontend/            worktree of frontend on task/add-billing
│       │   ├── AGENTS.md            generated from .forest/agents/*.md
│       │   ├── CLAUDE.md            (same content)
│       │   ├── .forest-task.yaml    metadata: which repos, branches, base
│       │   └── .env                 (optional, copied per env_files config)
│       └── fix-auth/              ← another task, same shape
│           └── ...
└── ...
```

## Why this works for AI agents

When you run `forest agent start add-billing`, the tool `chdir`s into the task
directory (`.forest/worktrees/add-billing/`) and execs the configured agent
(e.g. `claude`). The agent's working directory therefore contains:

- `AGENTS.md` / `CLAUDE.md` — the project-wide context, plus a header listing
  every repo in this task with its branch and base.
- `backend/`, `frontend/`, ... — sibling subdirectories that look like normal
  checkouts but are git worktrees on `task/add-billing`. The agent can `cd`
  between them, edit files, run `git status`, commit — all changes are on the
  shared task branch.
- env files at the locations you configured (e.g. `backend/.env`).

Crucially, the worktrees are real, so when the agent runs `git push` from
`backend/` it pushes the `task/add-billing` branch on the backend repo, and
the same from `frontend/` pushes the same branch name on the frontend repo.
Two PRs, one logical change, one branch name to grep for.

## Anatomy of a task — what the tool actually does on `task create`

For each repo in the task, forest runs (roughly):

```
git -C <root>/<repo> worktree add \
    -b task/<task> \
    <root>/.forest/worktrees/<task>/<repo> \
    <base-branch>
```

If `task/<task>` already exists in that repo, it checks out the existing
branch instead of creating a new one (so you can resume an in-progress task
after removing its workspace and recreating it).

Then it:

1. Writes `.forest-task.yaml` recording every (repo, branch, base) tuple plus
   `created_at`.
2. Concatenates every `*.md` file under `.forest/agents/` (sorted by filename)
   into `AGENTS.md` and `CLAUDE.md` at the task root, with a header listing the
   repos.
3. Copies every entry under `env_files` in the config from project root to
   task workspace, refusing to overwrite drift unless `--force` is passed.

`forest task remove <name>` is the inverse: it refuses on dirty/unpushed
worktrees (unless `--force`), runs `git worktree remove` on each, deletes the
task branch in each repo (unless `--keep-branches`), prunes stale worktree
admin entries, then `rm -r`s the task directory.

## Why git worktrees and not separate clones?

Worktrees share the same `.git/objects` store as the original checkout, so
they're cheap (no re-fetching), commits in one are immediately visible in the
others, and there's no risk of forgetting to push a branch from the wrong
copy.

## Glossary

| Term | Meaning |
|------|---------|
| **project** | Directory containing `.forest/` and one or more repo checkouts. |
| **repo** | An entry under `repos:` in `config.yaml`. Resolved relative to the project root unless absolute. |
| **task** | A workspace directory under `.forest/worktrees/` containing one worktree per chosen repo and shared metadata. |
| **task branch** | The branch each repo's worktree is on. Default is `task/<task-name>`. |
| **base** | The branch the task branch was forked from (per-repo `default_base`, usually `main`). |
| **agents fragments** | Markdown files under `.forest/agents/` concatenated into `AGENTS.md`/`CLAUDE.md` for every task. |
| **env files** | Files copied from the project root into each task workspace (configured under `env_files:`). |
| **command set** | A named bag of command-line invocations under `agents:` or `editors:` in the config; each set has a default and a map of `name → {command, args}`. |

## Where to go next

- [`02-walkthrough.md`](./02-walkthrough.md) — hands-on, end-to-end run
  through every command on a freshly initialised project.
- [`03-command-reference.md`](./03-command-reference.md) — every command and
  every flag.
- [`04-code-tour.md`](./04-code-tour.md) — package-by-package walk through
  the source so you can confidently change it.
- [`05-bubble-tea-tui.md`](./05-bubble-tea-tui.md) — how the TUI is built;
  doubles as a Bubble Tea / Lipgloss / Huh primer grounded in this codebase.
- [`issues/`](./issues/) — security and correctness findings.
