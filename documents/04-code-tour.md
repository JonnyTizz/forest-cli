# 04 — Code walkthrough

A teaching walk through the codebase. We'll start at `main.go` and follow
the wiring all the way through to a real command call, explaining the Go
idioms as we encounter them. You should read this with the source open
in another window.

If a Go concept is new, I'll explain it the first time it appears and
then assume you've got it. If you want a deeper reference, the official
[Tour of Go](https://go.dev/tour/) and [Effective Go](https://go.dev/doc/effective_go)
are both excellent.

## Table of contents

1. [How a Go program is laid out](#1-how-a-go-program-is-laid-out)
2. [`main.go` and the Cobra command tree](#2-maingo-and-the-cobra-command-tree)
3. [The `cmd/` layer — registration with `init()`](#3-the-cmd-layer)
4. [The `internal/` layer, in dependency order](#4-the-internal-layer-in-dependency-order)
   - [`config` — YAML schema and validation](#41-internalconfig)
   - [`gitx` — talking to git via `os/exec`](#42-internalgitx)
   - [`safety`, `agents`, `envfiles`, `runner` — the small leaves](#43-the-small-leaf-packages)
   - [`project` — making sense of paths](#44-internalproject)
   - [`workspace` — the orchestrator](#45-internalworkspace)
   - [`tui` — Bubble Tea, glanced at](#46-internaltui)
5. [End-to-end: `forest task create add-billing --repos api,web`](#5-end-to-end-trace)
6. [Where to look for what](#6-where-to-look-for-what)

---

## 1. How a Go program is laid out

A Go project lives inside a *module*, declared in `go.mod`:

```
module github.com/JonnyTizz/forest

go 1.24.0
```

That first line is the module's import path. Every package inside the
module lives at `github.com/JonnyTizz/forest/<subdir>`. So when you see:

```go
import "github.com/JonnyTizz/forest/internal/config"
```

Go looks for a directory `internal/config/` underneath the module root and
expects `package config` at the top of every `.go` file in there.

Two conventions you'll see in this repo:

- **Packages map to directories.** One directory = one package. The
  package name is whatever the files in there declare with `package
  foo`. By convention it matches the directory name (`internal/config`
  contains `package config`).
- **`internal/` is a magic name.** Anything under `internal/` can only
  be imported by code that shares the same parent. So
  `github.com/JonnyTizz/forest/internal/config` is importable from
  anywhere inside `github.com/JonnyTizz/forest/...` but **not** from
  another module. This is how forest hides its plumbing from
  hypothetical external consumers.

Each `.go` file starts with the package it belongs to and the imports it
needs:

```go
package workspace

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
    ...
    "gopkg.in/yaml.v3"

    "github.com/JonnyTizz/forest/internal/agents"
    ...
)
```

Imports are grouped by `goimports` convention: stdlib first, then
third-party, then your own module's packages, separated by blank lines.

Two more facts that bite newcomers:

- **Capitalisation is access control.** Identifiers starting with an
  uppercase letter are exported (visible from other packages); lowercase
  ones are package-private. So `config.Repo` is reachable from anywhere;
  `config.dirExists` only exists inside `internal/config`.
- **There is no constructor or annotation magic.** What you see is what
  runs. The only "magic" is `init()` functions (covered next).

## 2. `main.go` and the Cobra command tree

Open [`main.go`](../main.go):

```go
package main

import "github.com/JonnyTizz/forest/cmd"

func main() { cmd.Execute() }
```

Three lines. `package main` makes this a binary (anything else is a
library). The `main` function is the entry point. It just calls
`cmd.Execute()`.

So `cmd.Execute` does all the work. From [`cmd/root.go`](../cmd/root.go):

```go
func Execute() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, "error:", err)
        os.Exit(1)
    }
}
```

It calls `rootCmd.Execute()`. `rootCmd` is a `*cobra.Command` — Cobra is
the CLI framework forest uses (the same one used by `kubectl`, `gh`,
`hugo`, etc.). The pattern:

- You build a tree of `*cobra.Command` values, one per verb (`forest`
  → `forest task` → `forest task create`).
- Each command has metadata (`Use`, `Short`, `Long`), zero or more flags,
  and a `RunE` (or `Run`) function that does the work.
- You call `.Execute()` on the root and Cobra parses argv, picks the
  right command, populates flags, and calls its `RunE`.

How does the tree get built? Look at the bottom of `cmd/root.go`:

```go
func init() {
    rootCmd.PersistentFlags().StringVar(&flagProjectRoot, "project-root", "", "...")
    rootCmd.PersistentFlags().BoolVar(&flagForce, "force", false, "...")
    rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "...")
}
```

This is the **first new Go idiom**: `init()`.

> Every package can declare any number of functions named `init() {}`,
> with no arguments and no return values. Go runs all `init`s in a
> package before that package's first export is used. Across packages,
> init order follows the dependency graph: a package's imports run
> their inits before yours.

So when `cmd.Execute` is called from `main`, every `init()` in
`package cmd` has already run. That's how the entire command tree
materialises without any explicit "register everything" loop.

For example, [`cmd/task.go`](../cmd/task.go):

```go
var taskCmd = &cobra.Command{
    Use:     "task",
    Short:   "Manage task workspaces",
    Aliases: []string{"tasks"},
}

func init() {
    rootCmd.AddCommand(taskCmd)
}
```

And [`cmd/task_create.go`](../cmd/task_create.go):

```go
var taskCreateCmd = &cobra.Command{
    Use:   "create <name>",
    ...
}

func init() {
    taskCreateCmd.Flags().StringSliceVar(&tcRepos, "repos", nil, "...")
    ...
    taskCmd.AddCommand(taskCreateCmd)
}
```

Two `init`s, two layers of attachment: `taskCmd → rootCmd`, then
`taskCreateCmd → taskCmd`. The order doesn't matter because both
package-level vars (`rootCmd`, `taskCmd`) are constructed before any
`init` runs — package vars are initialised first, then `init`s, then
`main`.

A small thing worth noticing: `var taskCmd = &cobra.Command{...}` is a
**pointer to a struct literal**. The `&` takes the address. Cobra's API
takes `*cobra.Command` everywhere because it needs to mutate the struct
(adding subcommands, etc.). If you wrote `var taskCmd = cobra.Command{}`
you'd have a value type and `taskCmd.AddCommand(...)` would mutate a
copy. Same thing as the pointer/value receiver distinction we'll meet
soon.

## 3. The `cmd/` layer

Every file under `cmd/` follows the same shape. Read [`cmd/task_create.go`](../cmd/task_create.go)
top-to-bottom and you'll see it:

```go
package cmd

import (
    "errors"
    "fmt"
    "os"

    "github.com/charmbracelet/huh"
    "github.com/spf13/cobra"

    "github.com/JonnyTizz/forest/internal/project"
    "github.com/JonnyTizz/forest/internal/workspace"
)
```

Imports — note the three groups (stdlib / third-party / our packages).

```go
var (
    tcRepos    []string
    tcBranch   string
    tcBase     string
    tcNoPrompt bool
)
```

Package-level state: the variables Cobra writes flag values into. The
`tc` prefix is just convention to avoid name collisions with vars in
sibling files (Go has no namespacing inside a package — every file in
`package cmd` shares the same global scope).

`var (...)` is a *grouped declaration* — the same as four `var` lines,
just compact.

```go
var taskCreateCmd = &cobra.Command{
    Use:   "create <name>",
    Short: "Create a new task workspace",
    Args:  cobra.ExactArgs(1),
    RunE:  runTaskCreate,
}
```

The Cobra command itself. `Use` is the usage line; `Args:
cobra.ExactArgs(1)` validates "exactly one positional arg" before
`RunE` runs. `RunE` (E for "errors") is the function Cobra calls when
this command fires; if it returns a non-nil error, Cobra propagates it
up to `Execute()` which prints and exits.

```go
func init() {
    taskCreateCmd.Flags().StringSliceVar(&tcRepos, "repos", nil, "...")
    taskCreateCmd.Flags().StringVar(&tcBranch, "branch", "", "...")
    taskCreateCmd.Flags().StringVar(&tcBase, "base", "", "...")
    taskCreateCmd.Flags().BoolVar(&tcNoPrompt, "no-prompt", false, "...")
    taskCmd.AddCommand(taskCreateCmd)
}
```

Wire the flags in (passing the address of each var so Cobra can write
into them), then attach to the parent command.

```go
func runTaskCreate(cmd *cobra.Command, args []string) error {
    name := args[0]
    if err := workspace.ValidateName(name); err != nil {
        return err
    }
    p, err := project.Load(flagProjectRoot)
    if err != nil {
        return err
    }
    ...
}
```

The work function. `args` is the slice of positional args that survived
the `cobra.ExactArgs(1)` validator (so we know `args[0]` is safe).

A few things to absorb here:

- **Multiple return values.** `project.Load(...)` returns `(*project.Project, error)`.
  `p, err := project.Load(...)` is the canonical pattern: get the value
  and the error in one shot.
- **`if err != nil { return err }` everywhere.** Yes, it's verbose. Yes,
  this is "the way." The compiler will not let you forget about an error
  unless you explicitly throw it away with `_`. After a while it stops
  feeling like noise; it starts feeling like the program literally
  describing every place a thing might go wrong.
- **`:=` vs `=`.** `:=` is short variable declaration — declare and
  assign. `=` is reassignment of an existing variable. Inside a function
  you'll mostly use `:=` for new vars and `=` to mutate them.

Skimming forward in `runTaskCreate`:

```go
repos := tcRepos
if len(repos) == 0 {
    if tcNoPrompt || !isTTY() {
        return errors.New("--repos is required in non-interactive mode")
    }
    all := workspace.SuggestRepoOrder(p)
    ...
    opts := make([]huh.Option[string], len(all))
    for i, r := range all {
        opts[i] = huh.NewOption(r, r).Selected(true)
    }
    var picked []string
    form := huh.NewForm(huh.NewGroup(
        huh.NewMultiSelect[string]().
            Title(...).
            Options(opts...).
            Value(&picked),
    ))
    if err := form.Run(); err != nil {
        return err
    }
    if len(picked) == 0 {
        return errors.New("no repos selected")
    }
    repos = picked
}
```

A couple more things:

- `make([]huh.Option[string], len(all))` allocates a slice with a known
  length. The square brackets in `huh.Option[string]` are **generics**
  — `huh.Option` is parameterised on the option's value type.
- `for i, r := range all` iterates a slice yielding both the index and
  the value. `for _, r := range all` would skip the index.
- `Options(opts...)` is a **variadic call** — `opts...` unpacks the
  slice into individual arguments. `Options` was declared as
  `func Options(opts ...Option[string])`.

After we have the repos list, we hand off to `workspace.Create`:

```go
t, err := workspace.Create(p, workspace.CreateOptions{
    Name:   name,
    Repos:  repos,
    Branch: tcBranch,
    Base:   tcBase,
})
```

`workspace.CreateOptions` is a *struct literal with named fields*.
This is the idiomatic way to call a function with several optional
parameters in Go (Go doesn't have keyword arguments).

Everything else in `cmd/` follows the same shape:

1. parse args/flags,
2. load the project,
3. resolve any prompt-or-flag inputs,
4. call into `internal/...`,
5. format the result.

Once you've internalised this, the `cmd/` layer is mostly bookkeeping.
The interesting code is in `internal/`.

## 4. The `internal/` layer, in dependency order

The dependency graph (X depends on Y means there's an `import` of Y in X):

```
project   ←  cmd/*
   │
config ──┤            envfiles  ──┐
   │     │               │        │
   │     ├──  workspace ─┤        ├──  cmd/*
gitx ────┤        │      │        │
         │      agents ──┤        │
safety ──┤      runner  ──────────┤
                                  │
                tui ──────────────┘
```

The leaves at the top of the diagram (`config`, `gitx`, the small ones)
have no dependencies on other forest packages. The ones below them
compose. We'll work bottom-up.

### 4.1. `internal/config`

[`internal/config/config.go`](../internal/config/config.go) defines the
on-disk schema and how to load/save/validate it.

```go
package config

const (
    Dirname                = ".forest"
    ConfigFile             = "config.yaml"
    AgentsDir              = "agents"
    DefaultWorktreesSubdir = "worktrees"
)
```

`const` blocks define compile-time constants. By convention exported
constants are `CamelCase`.

```go
type Repo struct {
    Name        string `yaml:"name"`
    Path        string `yaml:"path"`
    DefaultBase string `yaml:"default_base"`
}
```

This is a **struct type** with **struct tags** (the backtick strings
after each field). Struct tags are arbitrary metadata strings that
libraries can read at runtime. `gopkg.in/yaml.v3` looks for `yaml:"..."`
tags to map struct fields to YAML keys.

So this struct corresponds to:

```yaml
name: api
path: api
default_base: main
```

The mapping is automatic — neither side has to write any conversion
code.

```go
type ProjectConfig struct {
    Version      int        `yaml:"version"`
    Repos        []Repo     `yaml:"repos"`
    EnvFiles     []EnvFile  `yaml:"env_files,omitempty"`
    Agents       CommandSet `yaml:"agents"`
    Editors      CommandSet `yaml:"editors"`
    WorktreesDir string     `yaml:"worktrees_dir,omitempty"`
}
```

`omitempty` says "if the field is the zero value for its type, leave
this key out when marshaling." That's why a freshly-`init`'d config
that has no env files writes `repos: [...]` but no `env_files:` line.

Three more functions to know in this file:

```go
func Defaults() ProjectConfig { ... }     // builds the default ProjectConfig used by `init`
func Load(path string) (*ProjectConfig, error) { ... }
func Save(path string, c *ProjectConfig) error { ... }
func (c *ProjectConfig) Validate() error { ... }
```

`Load` reads the file, `yaml.Unmarshal`s it into the struct, then
calls `Validate()`. `Save` does the inverse. `Validate` is a
**method** (the `(c *ProjectConfig)` is the *receiver*) — it runs
checks like "version must be 1" and "repo names must be unique".

Receivers come in two flavours:

- `func (c *ProjectConfig) Validate() error` — *pointer receiver*. The
  method gets a pointer to the original struct and can mutate it.
- `func (c ProjectConfig) ReadOnly() string` — *value receiver*. The
  method gets a copy. It can't mutate the original.

forest uses pointer receivers for `*ProjectConfig` because `Validate`
fixes up empty `default_base` fields on the original struct in place:

```go
if r.DefaultBase == "" {
    c.Repos[i].DefaultBase = "main"
}
```

The other interesting method on `ProjectConfig`:

```go
func (c *ProjectConfig) FindRepo(name string) (Repo, bool) {
    for _, r := range c.Repos {
        if r.Name == name {
            return r, true
        }
    }
    return Repo{}, false
}
```

The `(value, ok)` two-return pattern is everywhere in idiomatic Go —
maps and type assertions both use it. Callers do:

```go
r, ok := cfg.FindRepo("api")
if !ok { ... }
```

`Repo{}` is the **zero value** of `Repo`: a struct where every field
holds its type's zero value (`""` for strings, `0` for ints, `nil`
for slices/maps/pointers). Returning a zero value alongside `false`
is the conventional "not found" answer.

### 4.2. `internal/gitx`

[`internal/gitx/gitx.go`](../internal/gitx/gitx.go) is the only place
in the codebase that knows how to talk to `git`. Forest doesn't use a
Go git library; it shells out to the `git` CLI. The whole package is
~165 lines and worth reading top-to-bottom.

The core helper:

```go
func Run(dir string, args ...string) (string, error) {
    cmd := exec.Command("git", args...)
    cmd.Dir = dir
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("git %s: %w (%s)",
            strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
    }
    return strings.TrimRight(stdout.String(), "\n"), nil
}
```

A few fundamentals on display:

- `args ...string` is **variadic** — caller can pass any number of
  strings and they arrive as a `[]string`. Same syntax as
  `Options(opts...)` we saw earlier, just from the receiving end.
- `var stdout, stderr bytes.Buffer` declares two variables of the same
  type. `bytes.Buffer` is a stdlib byte buffer that satisfies
  `io.Writer`, which is what `cmd.Stdout` and `cmd.Stderr` expect.
- `&stdout` takes the address — `cmd.Stdout` wants an `io.Writer`, and
  `*bytes.Buffer` implements that interface (the value receiver methods
  are also accessible through a pointer).
- `fmt.Errorf("...%w...", ..., err, ...)` is **error wrapping**. The
  `%w` verb embeds `err` inside the new error so `errors.Is` and
  `errors.As` can unwrap it later. forest doesn't use unwrapping much
  but the pattern is idiomatic.

Every other function in the file is a thin wrapper that calls `Run`
with specific args and parses the result:

```go
func BranchExists(repoDir, branch string) bool {
    _, err := Run(repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
    return err == nil
}
```

(The `_` discards the empty stdout. We only care whether the command
succeeded.)

A more interesting one — `Stat` parses `git status --porcelain=v1`:

```go
type Status struct {
    Clean      bool
    Modified   int
    Staged     int
    Untracked  int
    Branch     string
    LastCommit string
}

func Stat(dir string) (*Status, error) {
    out, err := Run(dir, "status", "--porcelain=v1", "--untracked-files=normal")
    if err != nil {
        return nil, err
    }
    s := &Status{Clean: true}
    for _, ln := range strings.Split(out, "\n") {
        if len(ln) < 2 { continue }
        s.Clean = false
        x, y := ln[0], ln[1]
        switch {
        case x == '?' && y == '?':
            s.Untracked++
        case y != ' ' && y != 0:
            s.Modified++
            ...
        }
    }
    ...
    return s, nil
}
```

`switch { case ... }` with no expression after `switch` is a generic
boolean ladder — equivalent to `if/else if/else`, just nicer to read.
`x, y := ln[0], ln[1]` is **multiple assignment**: two `byte`s
extracted in one line.

The one bug in this file is in `WorktreeAdd` — see
[`issues/correctness.md`](./issues/correctness.md#1).

### 4.3. The small leaf packages

Four small packages that build on `gitx`/`config`/the stdlib:

#### `internal/safety`

```go
type DirtyError struct {
    Path   string
    Status *gitx.Status
}

func (e *DirtyError) Error() string { ... }
```

`DirtyError` is a struct that satisfies the `error` interface by having
an `Error() string` method. **Go interfaces are implicit**: there's no
`implements` keyword. If your type has all the methods the interface
requires, you've satisfied it. Since `DirtyError` has `Error() string`,
`*DirtyError` is an `error`.

The two functions in the package are `EnsureClean` and
`EnsureNoUnpushed`. They return either `nil` (everything's fine), an
`*DirtyError`/`*UnpushedError` (a known refusal), or some other error
(I/O failed). Callers can `errors.As` to distinguish:

```go
err := safety.EnsureClean(path)
var de *safety.DirtyError
if errors.As(err, &de) {
    fmt.Println("would refuse:", de.Path)
}
```

forest's `workspace.Remove` doesn't actually use `errors.As` — it just
returns the first refusal — but the typed errors are there for future
use.

#### `internal/agents`

`Render(agentsDir, meta TaskMeta) ([]byte, error)` is pure rendering:
it reads every `*.md` file under `agentsDir`, sorts them by filename,
and concatenates them under a header generated from `meta`. The output
is a `[]byte` so the caller decides what to do with it.

`WriteAll(taskDir, content)` writes `AGENTS.md` and `CLAUDE.md` (same
content, two filenames — different agents look for different names).

`bytes.Buffer` shows up here as well:

```go
var buf bytes.Buffer
writeHeader(&buf, meta)
for _, f := range files {
    ...
    fmt.Fprintf(&buf, "\n<!-- agents/%s -->\n\n", f)
    buf.Write(b)
    ...
}
return buf.Bytes(), nil
```

`fmt.Fprintf` writes formatted text to anything that implements
`io.Writer`. `buf.Write(b)` appends raw bytes. `buf.Bytes()` returns
the accumulated content.

#### `internal/envfiles`

Two top-level functions: `Sync` and `Diff`. Both take the project
root, the task dir, the list of `EnvFile` configs, and return per-file
results. They're decision tables — read the cases in
[`Sync`](../internal/envfiles/envfiles.go) one by one:

1. Source missing → `skipped-missing-source`.
2. Source present, dest matches → `skipped-identical`.
3. Dest exists & differs & `!force` → `refused-modified`.
4. Otherwise → write → `copied`.

The whole package is a clean example of "compute the answer, return a
list of structured results, let the caller print them." None of it
prints anything itself — that's `cmd/env_sync.go`'s job.

#### `internal/runner`

Three jobs:

```go
func Resolve(set config.CommandSet, name string) (config.CommandSpec, string, error)
```

Look up a name in a `CommandSet`, falling back to `set.Default`.
Returns the spec, the effective name (so we can print "Starting agent
\"claude\""), and an error if the name is unknown.

```go
func ExecReplace(cwd string, spec config.CommandSpec, extraArgs []string) error {
    bin, err := exec.LookPath(spec.Command)
    if err != nil { return ... }
    if err := os.Chdir(cwd); err != nil { return err }
    argv := append([]string{bin}, append(spec.Args, extraArgs...)...)
    return syscall.Exec(bin, argv, os.Environ())
}
```

`syscall.Exec` is the Go binding for the unix `execve(2)` syscall:
**replace the current process** with a new one. After it succeeds,
forest is gone and the agent is running in its place. That's why
`forest agent start` is so smooth — there's no parent process holding
on to the TTY.

`Spawn` is the alternative: `exec.Command(...).Run()` waits for the
child but keeps the forest process around. Used for editors that fork
themselves.

### 4.4. `internal/project`

[`internal/project/project.go`](../internal/project/project.go) has
just one struct and a handful of methods. Its job is "given a CWD or
override, find the forest project root and load its config":

```go
type Project struct {
    Root   string
    Config *config.ProjectConfig
}

func FindRoot(override string) (string, error) {
    if override != "" {
        ... // use override
    }
    cwd, err := os.Getwd()
    if err != nil { return "", err }
    dir := cwd
    for {
        if dirExists(filepath.Join(dir, config.Dirname)) {
            return dir, nil
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            return "", fmt.Errorf("no %s/ found in %s or any parent (run `forest init` first)", config.Dirname, cwd)
        }
        dir = parent
    }
}
```

This is the upward walk. Loop forever; each iteration check whether
the current directory contains `.forest/`; if it does, return; if not,
go up one level. The exit condition `parent == dir` means
`filepath.Dir` couldn't go any further (we hit `/`).

`Load` glues `FindRoot` and `config.Load` together:

```go
func Load(override string) (*Project, error) {
    root, err := FindRoot(override)
    if err != nil { return nil, err }
    cfg, err := config.Load(filepath.Join(root, config.Dirname, config.ConfigFile))
    if err != nil { return nil, fmt.Errorf("load config: %w", err) }
    return &Project{Root: root, Config: cfg}, nil
}
```

Notice how every command in `cmd/` starts with `p, err := project.Load(flagProjectRoot)`.
That's the canonical "I need to act on this project" prefix.

Then there are five small helpers that resolve config paths to filesystem paths:

| Method | Returns |
|--------|---------|
| `(*Project).ForestDir()` | `<root>/.forest` |
| `(*Project).AgentsDir()` | `<root>/.forest/agents` |
| `(*Project).WorktreesDir()` | absolute path of `config.WorktreesDir` |
| `(*Project).TaskDir(name)` | `<worktrees-dir>/<name>` |
| `(*Project).RepoPath(name) (string, error)` | absolute path of the configured repo |

Keeping that path-resolution logic in one place is what lets the rest
of the code take a `*Project` and never worry about whether
`worktrees_dir` is relative or absolute, or whether `repo.Path` was
specified absolutely.

### 4.5. `internal/workspace`

[`internal/workspace/workspace.go`](../internal/workspace/workspace.go)
is the orchestrator. It's the longest file in `internal/` (~340 lines)
and the most worth understanding because it composes everything below.

The core types:

```go
type RepoMeta struct {
    Name   string `yaml:"name"`
    Branch string `yaml:"branch"`
    Base   string `yaml:"base"`
}

type Meta struct {
    Name      string     `yaml:"name"`
    Repos     []RepoMeta `yaml:"repos"`
    CreatedAt time.Time  `yaml:"created_at"`
}

type Task struct {
    Meta Meta
    Dir  string
}
```

`Meta` is what gets serialised to `.forest-task.yaml` inside each task
directory. `Task` is `Meta` plus where it lives on disk; it's the
hydrated form everyone passes around.

```go
type CreateOptions struct {
    Name   string
    Repos  []string
    Branch string
    Base   string
}
```

Options struct for `Create`. This is a common pattern in Go: instead of
a function with five parameters, take a single options struct so
callers can write the field names at the call site.

The big function — `Create`:

```go
func Create(p *project.Project, opts CreateOptions) (*Task, error) {
    if err := ValidateName(opts.Name); err != nil { return nil, err }
    if len(opts.Repos) == 0 { return nil, errors.New("at least one repo must be selected") }
    taskDir := p.TaskDir(opts.Name)
    if _, err := os.Stat(taskDir); err == nil {
        return nil, fmt.Errorf("task %q already exists at %s", opts.Name, taskDir)
    }
    branch := opts.Branch
    if branch == "" { branch = "task/" + opts.Name }

    if err := os.MkdirAll(taskDir, 0o755); err != nil { return nil, err }

    meta := Meta{Name: opts.Name, CreatedAt: time.Now()}
    created := make([]string, 0, len(opts.Repos))

    for _, repoName := range opts.Repos {
        repoCfg, ok := p.Config.FindRepo(repoName)
        if !ok {
            rollback(p, created, branch)
            os.RemoveAll(taskDir)
            return nil, fmt.Errorf("repo %q not configured", repoName)
        }
        repoPath, err := p.RepoPath(repoName)
        if err != nil {
            rollback(p, created, branch); os.RemoveAll(taskDir)
            return nil, err
        }
        base := opts.Base
        if base == "" { base = repoCfg.DefaultBase }
        wtPath := filepath.Join(taskDir, repoName)
        newBranch := branch
        if gitx.BranchExists(repoPath, branch) {
            newBranch = ""
        }
        if err := gitx.WorktreeAdd(repoPath, wtPath, newBranch, base); err != nil {
            rollback(p, created, branch); os.RemoveAll(taskDir)
            return nil, fmt.Errorf("create worktree for %s: %w", repoName, err)
        }
        created = append(created, wtPath)
        meta.Repos = append(meta.Repos, RepoMeta{Name: repoName, Branch: branch, Base: base})
    }

    if err := writeMeta(taskDir, meta); err != nil { return nil, err }
    if err := RegenerateAgents(p, &meta, taskDir); err != nil {
        return nil, fmt.Errorf("write agents files: %w", err)
    }
    if _, err := envfiles.Sync(p.Root, taskDir, p.Config.EnvFiles, false); err != nil {
        return nil, fmt.Errorf("sync env files: %w", err)
    }
    return &Task{Meta: meta, Dir: taskDir}, nil
}
```

Lots to unpack:

- **`make([]string, 0, len(opts.Repos))`**: a slice with length 0 and
  capacity `len(opts.Repos)`. This pre-allocates the backing array so
  the upcoming `append`s never need to reallocate. Pure
  micro-optimisation; you can write `var created []string` and it'll
  work fine, just slightly slower for big repo sets.
- **`for _, repoName := range opts.Repos`**: idiomatic slice iteration
  with the index discarded.
- **`rollback(p, created, branch); os.RemoveAll(taskDir)`** appears
  three times. This is the manual rollback path: undo any worktrees
  we created, then remove the task dir we made. Go has `defer` for
  guaranteed cleanup, but the rollback here is **only on error** — on
  success we want the worktrees and dir to remain. So `defer` would be
  wrong; manual cleanup is the right tool.
- **`fmt.Errorf("...%w", err)`**: the same error-wrapping pattern from
  `gitx.Run`. The caller can `errors.Is` or `errors.As` to find the
  wrapped error if they care.

Beneath `Create`, the file has:

- `Remove` — the reverse, with safety gating on `force`.
- `Get(p, name)` — load one task by name.
- `List(p)` — list every task in the worktrees directory.
- `RegenerateAgents` / `RegenerateAllAgents` — regenerate
  `AGENTS.md`/`CLAUDE.md` for one or all tasks.
- `writeMeta` / `readMeta` — YAML round-trip the per-task metadata.
- `rollback` — the unwind helper.
- `IgnoreInExclude` — append a line to `.git/info/exclude`,
  idempotent. Only used by `cmd/init.go`.

Notice how thin every method is. The orchestration is mostly "call
gitx, then envfiles, then agents, return a Task." That's the payoff
for the layered design — every operation reads as a script.

### 4.6. `internal/tui`

Two files:

- [`app.go`](../internal/tui/app.go) — the model, update, view.
- [`styles.go`](../internal/tui/styles.go) — Lipgloss style values.

Bubble Tea has its own mental model and explaining it inline would
double the length of this doc. So this section just notes the wiring;
the conceptual deep-dive lives in
[`05-bubble-tea-tui.md`](./05-bubble-tea-tui.md).

The entry point:

```go
func Run(p *project.Project) error {
    m := newModel(p)
    prog := tea.NewProgram(m, tea.WithAltScreen())
    _, err := prog.Run()
    return err
}
```

`cmd/tui.go` calls this. The TUI uses `workspace.List`, `workspace.Remove`,
`envfiles.Sync`, and `gitx.Stat`/`gitx.AheadBehind` — exactly the same
internal API the CLI commands use. There's nothing TUI-specific in
`internal/workspace`. That's by design: it lets the TUI be a "thin
view" and prevents drift between the two front-ends.

## 5. End-to-end trace

Let's follow `forest task create add-billing --repos api,web` line by
line through every layer.

### Step 1 — `main`

`main()` calls `cmd.Execute()` (in [`cmd/root.go`](../cmd/root.go#L30)).
By the time `Execute` runs, every `init()` in `package cmd` has fired,
so `rootCmd` has subcommands, `taskCmd` has `taskCreateCmd` etc.

### Step 2 — Cobra parses argv

Cobra walks argv, matches `task` → `create`, validates
`cobra.ExactArgs(1)` (yes, we passed one positional arg `add-billing`),
parses `--repos api,web` into `tcRepos = ["api", "web"]`, then calls
`runTaskCreate(taskCreateCmd, []string{"add-billing"})`.

### Step 3 — `runTaskCreate`

[`cmd/task_create.go:37`](../cmd/task_create.go#L37) onwards:

1. Validates the name with `workspace.ValidateName("add-billing")` — passes.
2. Calls `project.Load(flagProjectRoot)`. `flagProjectRoot` is empty
   (we didn't pass `--project-root`), so `project.FindRoot` walks up
   from CWD looking for `.forest/`. Suppose it finds it at
   `/Users/me/work/myapp`. Then it `config.Load`s
   `/Users/me/work/myapp/.forest/config.yaml`, returning a `*Project`.
3. `repos := tcRepos = ["api", "web"]`. Length nonzero, skip the prompt.
4. Calls `workspace.Create(p, CreateOptions{Name: "add-billing", Repos: ["api", "web"], Branch: "", Base: ""})`.

### Step 4 — `workspace.Create`

[`internal/workspace/workspace.go:62`](../internal/workspace/workspace.go#L62):

1. `ValidateName` again (cheap; defence in depth).
2. `taskDir = p.TaskDir("add-billing") = "/Users/me/work/myapp/.forest/worktrees/add-billing"`.
3. `os.Stat` the task dir — `errors.Is(err, os.ErrNotExist)` so we
   know it doesn't exist. Proceed.
4. `branch = "task/add-billing"` (we didn't pass `--branch`).
5. `os.MkdirAll(taskDir, 0o755)` creates the directory tree.
6. Initialise `meta` with the task name and timestamp; allocate
   `created` slice for rollback.
7. **Loop over `opts.Repos`:**

   **Iteration 1, `repoName = "api"`:**
   - `p.Config.FindRepo("api")` returns `{Name: "api", Path: "api", DefaultBase: "main"}`, ok=true.
   - `p.RepoPath("api")` returns `/Users/me/work/myapp/api`.
   - `base = "main"` (from repoCfg.DefaultBase).
   - `wtPath = /Users/me/work/myapp/.forest/worktrees/add-billing/api`.
   - `gitx.BranchExists(.../api, "task/add-billing")` — fresh, returns `false`.
   - `newBranch = "task/add-billing"`.
   - `gitx.WorktreeAdd(/users/.../api, .../api, "task/add-billing", "main")`
     runs `git -C /users/.../api worktree add -b task/add-billing .../api main`.
   - `created = [.../add-billing/api]`, `meta.Repos` has one entry.

   **Iteration 2, `repoName = "web"`:** same shape, runs the same git
   command in `/users/.../web`. Now `created` has two entries.

8. `writeMeta(taskDir, meta)` marshals `meta` to YAML, writes
   `.forest-task.yaml`.
9. `RegenerateAgents(p, &meta, taskDir)` →
   `agents.Render(p.AgentsDir(), TaskMeta{...})` → reads every
   `*.md` under `.forest/agents/`, sorts, concatenates with a
   header → `agents.WriteAll(taskDir, content)` writes both
   `AGENTS.md` and `CLAUDE.md`.
10. `envfiles.Sync(p.Root, taskDir, p.Config.EnvFiles, false)` —
    `p.Config.EnvFiles` is empty (default config), so no-op.
11. Returns `&Task{Meta: meta, Dir: taskDir}, nil`.

### Step 5 — back in `runTaskCreate`

`workspace.Create` returned a non-nil `*Task` and a nil error. We
print:

```
Created task "add-billing" at /Users/.../forest-demo/.forest/worktrees/add-billing
  api @ task/add-billing (from main)
  web @ task/add-billing (from main)
```

Return nil. Cobra propagates that nil up to `Execute`, which sees no
error and returns. `main` exits 0.

### What you should take away

- Every command in `cmd/` is a *thin* wrapper around `project.Load(...)`
  and one or two `internal/...` calls.
- `internal/workspace.Create` is an *orchestrator* — it composes
  smaller packages (`gitx`, `agents`, `envfiles`) and contains the
  rollback logic.
- The smaller packages each do one thing: `gitx` runs git, `agents`
  builds markdown, `envfiles` syncs files, `safety` returns typed
  refusal errors.
- Type information flows top-down (config → project → workspace →
  cmd) and the data flows the other way (gitx output → workspace
  metadata → cmd's println).

Once you've traced one command end-to-end like this, every other
command is shape-identical — you can usually skip straight to the
`internal/` function and skim the `cmd/` glue.

## 6. Where to look for what

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
| Where path resolution lives | `internal/project` (any path math should live here) |

## Adding a new command — a recipe

If you wanted to add `forest task open <name>` that just printed the
task directory:

Create [`cmd/task_open.go`](../cmd/) (the path doesn't yet exist):

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
        if err != nil {
            return err
        }
        t, err := workspace.Get(p, args[0])
        if err != nil {
            return err
        }
        fmt.Println(t.Dir)
        return nil
    },
}

func init() {
    taskCmd.AddCommand(taskOpenCmd)
}
```

That's it. The file is picked up by `package cmd`'s `init()` chain
automatically; `forest task open <name>` works on the next build.

If the command needs new behaviour beyond a thin wrapper around an
existing `internal/` function, add the logic to `internal/` first,
keep `cmd/` thin. That's the rule that keeps this codebase legible.

## Where to go next

- [`05-bubble-tea-tui.md`](./05-bubble-tea-tui.md) — same teaching
  style, applied to the Bubble Tea TUI. Read after this one if you
  want to extend the TUI.
- [Effective Go](https://go.dev/doc/effective_go) — Go's idiom guide,
  short and excellent.
- [`issues/correctness.md`](./issues/correctness.md) — the bug to fix
  in `gitx.WorktreeAdd` is a great first PR if you want to get hands
  on with this code. The fix touches `internal/gitx` and
  `internal/workspace` — the two layers that this doc spent the most
  time on.
