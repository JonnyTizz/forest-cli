# Correctness review: forest-cli

A sibling to [`security.md`](./security.md). Same review pass; this file
covers correctness, UX, and code-quality findings rather than security
issues. Severity reflects "how likely is this to bite a user in normal use",
not exploitability.

## HIGH

### 1. Re-using an existing task branch checks out the wrong branch — [internal/gitx/gitx.go:48-60](../../internal/gitx/gitx.go#L48), [internal/workspace/workspace.go:103-110](../../internal/workspace/workspace.go#L103)

When `forest task create` finds that the task branch already exists in a
repo, the intended behaviour (per the comment) is to check out the existing
branch into the new worktree. But the actual call path is:

```go
// workspace.go:103-110
newBranch := branch
if gitx.BranchExists(repoPath, branch) {
    newBranch = "" // check out existing
}
if err := gitx.WorktreeAdd(repoPath, wtPath, newBranch, base); err != nil {
```

```go
// gitx.go:48-60
func WorktreeAdd(repoDir, path, newBranch, base string) error {
    args := []string{"worktree", "add"}
    if newBranch != "" {
        args = append(args, "-b", newBranch, path)
        if base != "" { args = append(args, base) }
    } else {
        args = append(args, path, base)        // ← uses BASE, not the existing branch
    }
    ...
}
```

When `newBranch` is empty, the resulting git command is
`git worktree add <path> <base>`, which checks out **base** (e.g. `main`),
not the existing task branch.

**Concrete failure mode**: imagine you create task `foo` covering repos
`api` and `web`, and the branch `task/foo` already exists in `web` from a
previous session but not in `api`. You expect both worktrees to land on
`task/foo`. Actually `api` ends up on `task/foo` (because `newBranch` is
non-empty for it), but `web` ends up on `main` — and any commits you make in
the `web/` worktree will be on `main`, not on `task/foo`. The metadata file
will incorrectly claim `branch: task/foo` for `web`.

**Fix sketch**:

```go
// workspace.go
commitish := base
if gitx.BranchExists(repoPath, branch) {
    newBranch = ""
    commitish = branch  // check out the existing branch directly
}
if err := gitx.WorktreeAdd(repoPath, wtPath, newBranch, commitish); err != nil { ... }
```

Or rename the `gitx.WorktreeAdd` parameter from `base` to `commitish` so the
distinction is clearer at the API surface — the third argument is "what
ref/branch to base the new worktree on", which differs from "the base branch
the metadata records".

A regression test would be valuable here once tests are added. Currently
nothing exercises this branch.

---

### 2. No tests anywhere

`find . -name '*_test.go'` returns nothing. The packages that most need
tests are:

- `internal/workspace` — `Create`/`Remove`/`rollback` orchestration is the
  highest-stakes code in the codebase.
- `internal/gitx` — could be exercised against a `t.TempDir()` with real git
  init.
- `internal/envfiles` — `Sync` and `Diff` decision tables are easy to enumerate.
- `internal/agents` — `Render` produces deterministic output for a given
  input directory.

Without tests, the bug in #1 above survived the "vibe-code" pass undetected.
Adding even a single integration test (init → create → status → remove on
two real repos in `t.TempDir()`) would prevent the worst regressions.

---

## MEDIUM

### 3. `--verbose / -v` is registered but never read — [cmd/root.go:27](../../cmd/root.go#L27)

```go
rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Verbose output")
```

Nothing in the codebase reads `flagVerbose` or calls `Verbose()`. Either
remove the flag or thread it through to enable per-step logging in the
chatty paths (`workspace.Create`, `envfiles.Sync`, `gitx.Run`).

---

### 4. `--branch` is not validated — [cmd/task_create.go](../../cmd/task_create.go), [internal/workspace/workspace.go](../../internal/workspace/workspace.go)

The task **name** is regex-validated (`ValidateName`), but a user-supplied
`--branch` is passed straight to `git worktree add -b`. Git accepts most
strings here, but a name like `--no-verify` could be interpreted as a flag
(`gitx.WorktreeAdd` does not insert a `--` separator). Same concern is
flagged in [`security.md` #7](./security.md#7).

For correctness specifically: a branch name containing a slash is fine for
git (that's how `task/foo` works), but a name with leading `-` will look
like a flag. At minimum, prepend `--` before user-controlled positional
arguments in `gitx`.

---

### 5. Stored `Base` can disagree with reality — [internal/workspace/workspace.go:113](../../internal/workspace/workspace.go#L113)

When the task branch already existed (#1 path) the metadata still records
`Base: base` even though the worktree wasn't actually based on `base`. The
`forest task status` "ahead/behind vs base" column will then show numbers
relative to a base that was never the fork point. Cosmetic, but
misleading.

---

### 6. `Remove` continues silently after partial failure — [internal/workspace/workspace.go:148-165](../../internal/workspace/workspace.go#L148)

If `git worktree remove` fails for one repo (e.g. it's locked, or
forest's CLI doesn't have permission), the loop emits a `warn:` to stderr
and continues. This is intentional — better to clean up the rest than to
leave the user fully stuck — but the final `os.RemoveAll(t.Dir)` will
**still delete the task directory**, including the failed worktree's `.git`
file pointer. The next `git worktree prune` from the original repo will
then clean up the dangling reference, but until that happens the original
repo will think the worktree still exists.

A safer flow:
1. Remove all worktrees (collecting errors).
2. If any failed, refuse to `os.RemoveAll(t.Dir)` and return a combined error.
3. Otherwise prune branches and delete the task dir.

The current behaviour is acceptable but worth being aware of.

---

### 7. `pickTask` lists tasks by config order, not most-recent — [cmd/agent_start.go:66-69](../../cmd/agent_start.go#L66)

`workspace.List` sorts alphabetically. For the `pickTask` prompt that's
fine, but users typically want "most recently created" first. The metadata
includes `CreatedAt`; sorting descending by that for the prompt would be a
small UX win.

---

### 8. `gitx.CurrentBranch` swallows errors — [internal/gitx/gitx.go:38-44](../../internal/gitx/gitx.go#L38)

```go
func CurrentBranch(dir string) (string, error) {
    out, err := Run(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
    if err != nil {
        return "", nil   // ← non-nil err discarded
    }
    return out, nil
}
```

A detached HEAD (or any other genuine git failure) returns
`("", nil)`. That's only used in the TUI/status display where a blank
branch column won't crash anything, but it's confusing — the function
signature claims to return errors and never does. Either:

- always return the error, and let the caller decide to ignore it for
  detached HEADs, or
- change the signature to `func CurrentBranch(dir string) string` so the
  swallowing is explicit.

---

## LOW

### 9. `splitLines` reinvents `strings.Split` — [internal/workspace/workspace.go:321-336](../../internal/workspace/workspace.go#L321)

`splitLines` builds strings character-by-character with `cur += string(r)`
in a loop — quadratic in line length. Just call `strings.Split(s, "\n")`,
or `bufio.Scanner`, depending on how trailing-newline you want to be.
Not a real performance issue (used once on `.git/info/exclude`), just dead
weight.

### 10. `splitKeepNL` is also bespoke — [internal/envfiles/envfiles.go:159-176](../../internal/envfiles/envfiles.go#L159)

Same critique. The `unifiedLineDiff` algorithm is a line-by-line zip and
not actually a unified diff in the patch(1) sense — for env files that's
fine, but the function name oversells. Consider renaming to
`naiveLineDiff` or replacing with `github.com/sergi/go-diff` / similar if
you want correct hunk handling.

### 11. No way to update a task's repo set — `internal/workspace/workspace.go`

If a project gains a new repo, existing tasks don't pick it up. The user
has to `task remove` and `task create` again. A `forest task add-repo
<task> <repo>` would be a useful operation: `git worktree add` for the new
repo, append to `meta.Repos`, regenerate agents.

### 12. `WorktreesDir` cleanup on init — [cmd/init.go:101-103](../../cmd/init.go#L101)

`init` `os.MkdirAll`s the worktrees dir even when `--force` is overwriting
an existing project. If the user reduces the repo set on re-init, the
existing worktree directory is left behind, with stale tasks for repos
that no longer exist in the config. Probably worth a friendly notice on
re-init that lists tasks not in the new config.

### 13. `forest editor open` doesn't show errors well

`runner.Spawn` waits for the editor to exit. For GUI editors that fork &
detach this returns immediately, so there's no real wait, but on
shell-launched editors (vim, nvim) the spawn blocks. That might be the
intended behaviour for terminal editors but it's worth distinguishing the
two in config (e.g. `terminal: true` per editor option) or always exec'ing
when the editor is a known terminal program.

---

## Notes on fitness for purpose

The stated purpose ("shared worktrees across multiple repos so my agents
can interact") is well-served by the design. Specifically:

- **Each task is genuinely shared across repos**. The agent runs from the
  task root and has every repo as a sibling subdirectory, all on the same
  branch name.
- **The git worktree mechanism is the right primitive**. Forest doesn't
  invent a parallel checkout layer — it leans on a stable feature of git
  that already gets the file isolation, branch naming, and ref sharing
  right.
- **AGENTS.md / CLAUDE.md fan-out is correct**. The `agents/*.md`
  concatenation pattern is a known-good way to give every agent the same
  ambient context without per-task editing.
- **Env file sync with refusal-on-drift is the right default**. It avoids
  silently clobbering an in-flight task's edits.

The bug in #1 is the only thing standing between "this works" and "this
works correctly in the corner case where you re-create a task whose branch
still exists." Once that's fixed and the test suite is in place, this is a
production-quality 700-LoC tool.
