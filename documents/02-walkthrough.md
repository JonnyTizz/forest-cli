# 02 — Hands-on walkthrough

This doc takes you through every command in order, on a fresh project, so you
can see exactly what changes on disk at each step. Read [`01-overview.md`](./01-overview.md)
first if you haven't.

## 0. Build the binary

From the repo root:

```sh
go install ./...
```

That puts `forest` in `$GOPATH/bin` (typically `~/go/bin`). Make sure that's
on your `PATH`.

You can also just build a local binary with `go build -o forest .` and call
it as `./forest`.

## 1. Set up a playground project

Create a project root with two toy git repos:

```sh
mkdir -p ~/tmp/forest-demo && cd ~/tmp/forest-demo

for repo in api web; do
    mkdir $repo && (cd $repo && git init -q -b main && \
        echo "# $repo" > README.md && \
        git add . && git -c user.email=demo@x -c user.name=demo commit -qm init)
done

ls
# api/  web/
```

You now have two sibling repos on `main`, both with one commit.

## 2. `forest init`

```sh
forest init
```

Interactive: it detects every immediate subdirectory that contains `.git/`
(here: `api`, `web`), shows a multi-select form, and lets you pick which to
track. After confirming, it creates:

```
.forest/
├── config.yaml
└── agents/
    └── 00-overview.md
```

…and adds `.forest/worktrees` to each repo's `.git/info/exclude` so git won't
nag you about the new directory inside the working tree.

To skip the prompt:

```sh
forest init --no-prompt --repos api,web
```

Look at the config it wrote:

```yaml
# .forest/config.yaml
version: 1
repos:
  - name: api
    path: api
    default_base: main
  - name: web
    path: web
    default_base: main
agents:
  default: claude
  options:
    claude:
      command: claude
    aider:
      command: aider
editors:
  default: cursor
  options:
    cursor:
      command: cursor
      args: ["."]
    code:
      command: code
      args: ["."]
worktrees_dir: .forest/worktrees
```

You'll typically edit this file by hand to:

- change `default_base` per repo,
- add `env_files:` entries (see step 6),
- add or remove agent / editor variants,
- override `worktrees_dir` if you don't want it under `.forest/`.

## 3. `forest task create`

Create a task that touches both repos:

```sh
forest task create add-billing --repos api,web
```

(Without `--repos` you get a multi-select form; with `--no-prompt` and no
`--repos`, the command errors. Inside non-TTY contexts — CI, agent shells —
`--repos` is required.)

Output:

```
Created task "add-billing" at /Users/.../forest-demo/.forest/worktrees/add-billing
  api @ task/add-billing (from main)
  web @ task/add-billing (from main)
```

Inspect what was created:

```sh
ls .forest/worktrees/add-billing/
# AGENTS.md  CLAUDE.md  api/  web/  .forest-task.yaml
```

`.forest-task.yaml` records exactly what got created:

```yaml
name: add-billing
repos:
    - name: api
      branch: task/add-billing
      base: main
    - name: web
      branch: task/add-billing
      base: main
created_at: 2026-05-08T...Z
```

`AGENTS.md` has a generated header followed by the contents of every
`*.md` file under `.forest/agents/`. The header looks like:

```markdown
# Task: add-billing

_Created 2026-05-08T..._

## Repos in this workspace

| Repo | Branch | Base |
|------|--------|------|
| api  | task/add-billing | main |
| web  | task/add-billing | main |

---

<!-- agents/00-overview.md -->

# Project overview
...
```

Each `<repo>/` subdirectory is a real git worktree:

```sh
cd .forest/worktrees/add-billing/api && git status
# On branch task/add-billing
# nothing to commit, working tree clean

cd ../web && git branch --show-current
# task/add-billing
```

Commits, pushes, branch operations behave exactly like a normal checkout.

### What `--branch` and `--base` do

```sh
forest task create reuse-feature --repos api --branch feature/x --base develop
```

- `--branch` overrides the default `task/<name>` branch name.
- `--base` overrides the per-repo `default_base`. Useful when forking from
  something other than `main` for a one-off task.

If the named branch already exists in a repo, forest checks out the existing
branch into the new worktree instead of recreating it. (See
[`issues/correctness.md`](./issues/correctness.md#1) for a known bug on this
path.)

## 4. `forest task list` and `forest task status`

```sh
forest task list
# NAME         REPOS    BRANCHES                          DIRTY  CREATED
# add-billing  api,web  task/add-billing,task/add-billing        2026-05-08
```

Add `--json` for machine-readable output (used by scripts and other tooling).

For per-repo state including ahead/behind vs base and last commit subject:

```sh
forest task status              # all tasks
forest task status add-billing  # one task
# TASK         REPO  BRANCH            AHEAD/BEHIND  DIRTY  LAST
# add-billing  api   task/add-billing  +0/-0         clean  init
# add-billing  web   task/add-billing  +0/-0         clean  init
```

## 5. `forest agent start` — running an AI agent

```sh
forest agent start add-billing
# Starting agent "claude" in /Users/.../.forest/worktrees/add-billing
```

Under the hood this `chdir`s to the task directory then `syscall.Exec`s the
configured agent (`claude` by default). Process replacement (not spawning)
means signals, stdin/stdout/stderr, and the TTY all flow naturally — `Ctrl+C`
goes straight to the agent and there's no forest process sitting in between.

Pass `--agent <name>` to use a non-default entry from `config.agents.options`,
e.g. `forest agent start add-billing --agent aider`.

If you omit the task name and you're in a TTY, you get a single-select prompt.

## 6. `forest env sync` and `forest env diff`

Edit `.forest/config.yaml` to declare a couple of env files:

```yaml
env_files:
  - source: api/.env.example
    dest:   api/.env
  - source: shared.env
    dest:   shared.env
```

`source` is relative to the project root; `dest` is relative to the task
workspace. Now create those source files at the project root:

```sh
echo "API_KEY=demo" > api/.env.example
echo "SHARED=hi"    > shared.env
```

Sync them into every task (or one task):

```sh
forest env sync                # all tasks
forest env sync add-billing    # just this one
# [add-billing] /.../api/.env.example -> /.../add-billing/api/.env: copied
# [add-billing] /.../shared.env -> /.../add-billing/shared.env: copied
```

If you later edit the destination copy directly, sync refuses to overwrite:

```sh
echo "API_KEY=tampered" >> .forest/worktrees/add-billing/api/.env
forest env sync add-billing
# ...api/.env: refused-modified
# Error: 1 destination file(s) refused — they have local edits; re-run with --force to overwrite
```

`forest env diff` shows the drift without touching anything (and exits with
code 2 if any drift exists, useful for CI):

```sh
forest env diff add-billing
# [add-billing] .../api/.env.example ↔ .../api/.env: drift
# --- api/.env.example
# +++ api/.env
# +API_KEY=tampered
```

## 7. `forest editor open` — open in your IDE

```sh
forest editor open add-billing
# Opening "cursor" in /Users/.../.forest/worktrees/add-billing
```

This uses `Spawn` (not `Exec`) because GUI editors typically fork and detach,
so you stay in your shell.

## 8. `forest config edit agents` — fan-out context to every task

Open `.forest/agents/` in `$EDITOR` (or `$VISUAL`, or `vi`):

```sh
forest config edit agents
```

When you exit the editor, forest regenerates `AGENTS.md` and `CLAUDE.md` in
**every** task workspace. This is how you propagate updated context to
in-flight tasks without recreating them.

Add a frontend-specific fragment:

```sh
cat > .forest/agents/10-web.md <<'EOF'
## Web (frontend)

- Uses React + Vite
- Test command: `cd web && pnpm test`
EOF
forest config edit agents      # save & quit immediately to regenerate
```

Then:

```sh
grep -A2 "Web (frontend)" .forest/worktrees/add-billing/CLAUDE.md
# ## Web (frontend)
# - Uses React + Vite
```

## 9. `forest tui` — interactive task explorer

```sh
forest tui
```

A two-pane Bubble Tea screen: tasks on the left, details (per-repo branch,
ahead/behind, dirty state) on the right. Keys:

| Key | Action |
|-----|--------|
| `j` / `↓` | move cursor down |
| `k` / `↑` | move cursor up |
| `r` | reload |
| `s` | env sync the highlighted task (without `--force`) |
| `d` | remove the highlighted task (refuses if dirty) |
| `q` / `Esc` / `Ctrl+C` | quit |

The TUI is intentionally read-mostly. Creation, force-removal, and most
flags live in the CLI. See [`05-bubble-tea-tui.md`](./05-bubble-tea-tui.md)
for how it's built.

## 10. `forest task remove` — clean up

```sh
forest task remove add-billing
# Removed task "add-billing"
```

Refuses if any worktree is dirty or has unpushed commits. Pass `--force` to
override. Pass `--keep-branches` to keep the task branches in each repo (the
default is to delete them with `git branch -d`, or `-D` under `--force`).

After removal:

```sh
ls .forest/worktrees/    # (empty)
git -C api branch        # main
```

## A typical loop

```sh
forest task create my-feature --repos api,web
forest agent start my-feature
# … agent edits files, commits in api/ and web/ …
forest task status my-feature   # see what's dirty / ahead
# manually `git push` from each subdir, or do it inside the agent session
forest task remove my-feature
```

That's the entire happy path.
