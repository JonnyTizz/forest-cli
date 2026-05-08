# Security Audit: forest-cli

## CRITICAL

### 1. Shell Injection via `$EDITOR` — [cmd/config_edit.go:38](../../cmd/config_edit.go#L38)

```go
c := exec.Command("sh", "-c", editor+` "$@"`, "sh", p.AgentsDir())
```

The raw `$EDITOR` env var is concatenated directly into a `sh -c` string. If `EDITOR` is set to `vim; rm -rf ~` (e.g., by a malicious script that modified your environment before you ran `forest config edit agents`), both commands execute. The fix is to split EDITOR into command + args and use `exec.Command` directly instead of `sh -c` string concatenation.

---

### 2. Path Traversal in env file sync — [internal/envfiles/envfiles.go:117-121](../../internal/envfiles/envfiles.go#L117)

```go
func absUnder(root, p string) string {
    if filepath.IsAbs(p) {
        return p         // ← absolute paths escape root entirely
    }
    return filepath.Join(root, p)  // ← ../.. sequences resolve out of root
}
```

A `forest.yaml` with `source: /etc/passwd` or `source: ../../.ssh/id_rsa` will read those files and copy them into your task directory. This matters if you ever open a project with a `forest.yaml` you didn't write yourself (cloned repos, shared templates). The fix requires rejecting absolute paths and calling `filepath.Clean` + a prefix check to ensure the result stays under `root`.

---

## HIGH

### 3. `os.RemoveAll` on task directories — [internal/workspace/workspace.go:89,95,109](../../internal/workspace/workspace.go#L89)

Task names are validated by `nameRe` (lowercase, digits, `.`, `_`, `-` only) so the path can't escape the tasks directory. The validation is solid and this does **not** appear exploitable as written — severity is effectively Medium in practice.

### 4. Unvalidated `path` field in repo config — [internal/config/config.go](../../internal/config/config.go)

The `Repo.Path` field (which points to local git repos) is not restricted to relative paths or paths within the project. A malicious `forest.yaml` with `path: /` or `path: ../../../../some-system-dir` would have git worktree operations (`git worktree add`, `git worktree remove --force`) run against arbitrary directories on your filesystem.

---

## MEDIUM

### 5. TOCTOU on task directory creation — [internal/workspace/workspace.go:70-78](../../internal/workspace/workspace.go#L70)

`os.Stat` check then `os.MkdirAll` — another process could create the directory between them. Low real-world risk for a local CLI but could cause confusing errors in parallel invocations.

### 6. World-readable config and metadata files — Multiple locations

Files written with `0o644` (readable by all users on the system):
- `.forest/forest.yaml` — contains repo paths and env file config
- `.forest-task.yaml` — task metadata
- Generated `AGENTS.md`/`CLAUDE.md` in each task

On a shared or multi-user machine this leaks your project structure and agent configurations. If those env files contain secrets (API keys, `.env` files synced into tasks), they're exposed.

### 7. Git option injection via branch/worktree names — [internal/gitx/gitx.go](../../internal/gitx/gitx.go)

Arguments are passed via array (safe from shell injection), but a branch name like `--no-verify` or `-f` could be interpreted as flags by git. In practice git's `--end-of-options` (`--`) separator is missing. Low exploitability but worth adding `--` before user-controlled branch name arguments.

---

## LOW

### 8. `absUnder` also accepts absolute `Dest` paths

Same function is used for the destination path. A config with `dest: /tmp/exfil` would write the copied env file outside the task directory entirely.

---

## Summary Table

| # | Severity | Finding | File |
|---|----------|---------|------|
| 1 | Critical | Shell injection via `$EDITOR` in `sh -c` | [cmd/config_edit.go:38](../../cmd/config_edit.go#L38) |
| 2 | Critical | Path traversal in env file sync (read/write arbitrary files) | [internal/envfiles/envfiles.go:117](../../internal/envfiles/envfiles.go#L117) |
| 3 | High | Unvalidated `Repo.Path` allows git ops on arbitrary dirs | [internal/config/config.go](../../internal/config/config.go) |
| 4 | Medium | TOCTOU on task dir creation | [internal/workspace/workspace.go:70](../../internal/workspace/workspace.go#L70) |
| 5 | Medium | Metadata/config files world-readable (0o644) | Multiple |
| 6 | Medium | Git args missing `--` option terminator | [internal/gitx/gitx.go](../../internal/gitx/gitx.go) |
| 7 | Low | Absolute `Dest` paths in env file config not restricted | [internal/envfiles/envfiles.go](../../internal/envfiles/envfiles.go) |

**The two issues that could actually hurt you:** #1 only fires if your `$EDITOR` env var gets poisoned before you run `forest config edit agents`. #2 is the more realistic risk — if you ever run forest against a repo you didn't fully trust, a malicious `forest.yaml` could exfiltrate files like `~/.ssh/id_rsa` or `~/.netrc` into the task workspace where another tool might upload them.
