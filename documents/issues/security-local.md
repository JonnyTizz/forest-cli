# Security review under the "local single-user dev machine" threat model

A reassessment of [`security.md`](./security.md) from a more honest threat
model: forest only ever runs on your own laptop, you're the only user, and
this isn't a network service. That changes which findings actually matter
to you.

This doc is **not** about lowering severities to make you feel better. It's
about being precise: some abstract findings genuinely don't matter for
local use, and pretending they do drowns out the ones that do.

## Threat model

Realistic attackers, in decreasing likelihood:

1. **A hostile `.forest/config.yaml`** you didn't write — copied from a
   "starter template" repo, cloned alongside someone else's project, or
   pasted from a blog post. Your AI agent runs inside the resulting task
   workspace.
2. **A compromised dependency** (npm postinstall, a malicious VS Code
   extension, a poisoned dotfile) that mutates your env vars before forest
   reads them.
3. **The AI agent itself** behaving badly — accidentally exfiltrating a
   secret you copied into a task workspace via `env_files`.

Out of scope:

- Other users on your machine (you don't have any).
- A network-exposed forest (it's never network-exposed).
- A nation-state with a kernel exploit.

## Reassessed findings

### #1 — Shell injection via `$EDITOR` (was Critical)

**Effective severity: LOW.**

The attack requires `$EDITOR` to already contain a malicious string at the
moment you run `forest config edit agents`. For that to happen,
**something else has already compromised your shell environment**: a
poisoned dotfile, a hostile shell plugin, an `npm postinstall` that wrote
to your `~/.zshrc`, a VS Code extension running with your credentials.
At that point, the attacker has full code execution as you anyway and
doesn't need this gadget — they could rewrite any of your binaries,
intercept your `gh` tokens, etc.

Worth fixing **as code hygiene** (the `sh -c "$EDITOR ..."` pattern is
intrinsically bad and tends to copy-paste itself elsewhere), but it's not
a meaningful new attack surface on a personal machine.

### #2 — Path traversal in env file sync (was Critical)

**Effective severity: MEDIUM, and the most realistic finding in this list.**

This is the one you actually need to think about. The attack:

1. You `git clone someone-elses-repo` that ships a `.forest/config.yaml`.
2. That config has `env_files: [{source: ../../.ssh/id_rsa, dest:
   stash/key}]`.
3. You run `forest task create demo --repos ...`.
4. forest reads your SSH private key and copies it into
   `.forest/worktrees/demo/stash/key`.
5. You run `forest agent start demo`. Your AI agent now has access to
   `stash/key` in its working directory and may read it as it explores
   the workspace, include it in tool-result context that gets sent to a
   model provider, or paste it into a commit.

Why this is more dangerous in 2026 than it was in 2010: AI agents
**autonomously read files**. A `forest task create` followed by `forest
agent start` is enough to surface anything `absUnder` will resolve.
That includes:

- `~/.ssh/id_rsa`, `~/.ssh/id_ed25519`
- `~/.aws/credentials`, `~/.aws/config`
- `~/.netrc`, `~/.gitconfig` (not secret but PII)
- `~/.config/<service>/credentials.json`
- Anything else the attacker bothered to enumerate.

Mitigation: `absUnder` should reject absolute paths and `..` traversal in
`source`. The fix is ten lines and worth doing. (Same applies to `dest`
per finding #8 — though absolute `dest` is less interesting because it
writes a copy of a file you already have, rather than reading something
new.)

In the meantime, treat any unfamiliar `.forest/config.yaml` the same way
you treat unfamiliar `Makefile` or `.envrc`: read it before the first
forest command runs.

### #3 — `os.RemoveAll` on task dirs (was High, already dismissed)

**Effective severity: NONE.** As the original audit notes, `nameRe`
prevents traversal, so this is fine.

### #4 — Unvalidated `Repo.Path` (was High)

**Effective severity: LOW.**

Same vector as #2 — needs a hostile config — but louder. A config with
`path: /` would have `git worktree add` and `git worktree remove
--force` operating on your home directory. You'd notice within seconds:
`git worktree add` on a non-repo directory fails noisily, and
`workspace.Create`'s rollback path then runs `WorktreeRemove` on the
attempted path which also fails. Hard to actually destroy data through
this path; easy to spot and abort.

Still worth fixing the same way you'd fix #2: `Repo.Path` should be
relative and resolved-under-root, with `..` rejected.

### #5 — TOCTOU on task dir creation (was Medium)

**Effective severity: NONE.** You're not racing yourself. Skip.

### #6 — World-readable config and metadata (was Medium)

**Effective severity: NONE for a personal laptop.** You're the only user.
On macOS your home directory is mode 0700 by default; on Linux your
distro likely does the same or you have a single-user system. The 0644
permissions on `.forest/config.yaml` are visible only to processes
running as you, which is exactly the population that could read it
anyway via `~/.forest/...`.

The only way this matters is if you sync `.forest/` to a multi-user host
(shared dev server, a Mac with multiple login accounts, a misconfigured
Linux box). If you do, audit per-host.

### #7 — Git option injection via branch/worktree names (was Medium)

**Effective severity: NONE for normal local use, LOW if you ever pipe in
branch names from elsewhere.**

For correctness you should still add `--` before user-controlled
positional args in `gitx` (this is also flagged in
[`correctness.md`](./correctness.md#4)), but in your day-to-day use
you're typing the `--branch` value yourself. You won't accidentally
type `--exec=touch /tmp/owned`.

### #8 — Absolute `Dest` paths in env files (was Low)

**Effective severity: LOW.** Same vector as #2; less interesting because
it writes rather than reads. Still fix it as part of fixing #2 —
`absUnder` should be the same hardened helper for both directions.

## New findings the original audit missed (or dismissed)

### A — Symlink-following in env file sync — [internal/envfiles/envfiles.go:31](../../internal/envfiles/envfiles.go#L31)

`os.ReadFile` follows symlinks. Even after fixing the absolute-path
problem in `absUnder`, a config with `source: api/.env` is still
unsafe if `api/.env` is a symlink to `~/.ssh/id_rsa`. Hostile templates
could pre-create the symlink as a tracked git symlink in the project
they ship.

Fix: `os.Lstat` the source first, refuse symlinks (or refuse symlinks
that resolve outside the project root). Since env files are tiny this
costs you nothing.

### B — The AI agent has direct access to env files copied into the task — operational

This isn't a forest bug; it's a property of the design you should be
aware of. When you put `dest: api/.env` and the source contains real
production credentials, those credentials end up at
`.forest/worktrees/<task>/api/.env`. The agent reads from there. From
the agent's perspective that file looks like just another piece of
context — it might:

- include it verbatim in a commit ("here's the env I think you need"),
- paste it into a chat that's sent to a model provider,
- echo it during a debug session that gets logged.

This is the point of `env_files` (so the app boots), so you can't avoid
it entirely. Mitigations:

- Keep `env_files` pointing at sanitised templates (`api/.env.example`,
  not `api/.env.production`) and have the agent fill in real values
  from a secret manager when it needs them.
- Add `*.env` to `.gitignore` inside each repo (you should be doing
  this anyway).
- Periodically `forest env diff` to spot drift — drift in a `.env`
  destination usually means the agent or you wrote credentials in.

### C — `runner.ExecReplace` calls `os.Chdir` before `exec` — [internal/runner/runner.go:35](../../internal/runner/runner.go#L35)

```go
if err := os.Chdir(cwd); err != nil { return err }
argv := append(...)
return syscall.Exec(bin, argv, os.Environ())
```

`os.Chdir` is process-global. If the `LookPath` succeeded (so we got past
the bin lookup) and `Chdir` succeeded, but `syscall.Exec` then failed for
any reason (rare — usually it's a permission/binary issue caught by
`LookPath`), the forest process is now sitting in the task directory
rather than wherever the user started.

Real impact: ~zero. `Exec` failure exits non-zero anyway. But the
ordering is fragile; doing `Chdir` after `LookPath` and immediately
before `Exec` means there's no recovery path if `Exec` itself fails.
If you ever change `ExecReplace` to recover-and-spawn-instead, remember
to `os.Chdir` back.

### D — `gopkg.in/yaml.v3` is fine — confirmed not exploitable

YAML deserialization in Go yaml.v3 doesn't construct arbitrary types
from tags the way PyYAML's `Loader` does. The struct-based unmarshal
forest uses for `ProjectConfig` and `Meta` can't be coerced into
arbitrary code execution. Worth confirming explicitly because this is a
common worry.

### E — `exec.LookPath("claude")` and friends trust `$PATH`

Same threat profile as the `$EDITOR` issue: only matters if `$PATH` is
already adversary-controlled, in which case the attacker doesn't need
forest to act. Code hygiene only.

## What to actually do, in priority order

If you're going to spend an afternoon on security, here's the order:

1. **Fix `absUnder` to reject absolute paths and `..` traversal in
   `source` and `dest`.** Add an `os.Lstat` symlink check while you're
   there. This closes #2, #8, and finding A in one PR. ~15 lines.
2. **Validate `Repo.Path` the same way** in `config.Validate()`. Closes
   #4. ~5 lines.
3. **Skip the rest unless you change deployment context.** #1, #6, #7
   only matter under threat models that don't apply to your laptop.
   Track them as code-hygiene items, not security work.

If you ever publish this tool for other people to use, all of the
"NONE for local use" findings shift up — the world-readable config in #6
becomes real on shared CI runners, the `$EDITOR` injection in #1 becomes
real on systems where ops set `$EDITOR` from a config-management tool,
and so on. Re-read the original [`security.md`](./security.md) at that
point.
