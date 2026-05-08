# 05 — Go TUIs: Bubble Tea, Lipgloss, and Huh

A teaching walk through the `internal/tui` package and the Charm
libraries it uses. Same style as
[`04-code-tour.md`](./04-code-tour.md): we'll explain the framework
concepts as we encounter them in the actual code, and stop to clarify
Go fundamentals when something needs it.

By the end you should be able to:

1. Read [`internal/tui/app.go`](../internal/tui/app.go) and
   [`internal/tui/styles.go`](../internal/tui/styles.go) line by line.
2. Add your own key binding to forest's TUI.
3. Recognise enough of the Charm idiom to read other Bubble Tea apps
   on GitHub.

## Three libraries, three jobs

forest uses three Charm packages. They're often used together but
each one is independent and has its own purpose:

| Library | What it does | Where forest uses it |
|---------|-------------|----------------------|
| **Lipgloss** | Strings → styled strings (colours, padding, borders, side-by-side joins). | `internal/tui/styles.go`, `View()`. |
| **Bubble Tea** | A framework for full-screen interactive programs. The Elm Architecture in Go: `Model` + `Update` + `View`. | The whole `forest tui` command. |
| **Huh** | Forms — single-select, multi-select, text input — built on top of Bubble Tea. | The interactive prompts in `forest init`, `forest task create`, `forest agent start`. |

You don't have to use all three. Lipgloss is happy on its own (use it
for styled `fmt.Println`). Huh is a one-shot wrapper around Bubble
Tea — forest uses it without ever instantiating a Bubble Tea program
directly, in `cmd/init.go` and friends. Bubble Tea is the heaviest of
the three; you only reach for it when you want a long-lived
keyboard-driven screen.

## 1. Lipgloss — pure functions over strings

Start with [`internal/tui/styles.go`](../internal/tui/styles.go). The
whole file:

```go
package tui

import "github.com/charmbracelet/lipgloss"

var (
    colorPrimary = lipgloss.Color("#5FAF87")
    colorMuted   = lipgloss.Color("#6c6c6c")
    colorDanger  = lipgloss.Color("#D75F5F")
    colorAccent  = lipgloss.Color("#87AFD7")

    titleStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("#ffffff")).
            Background(colorPrimary).
            Bold(true).
            Padding(0, 1)

    paneStyle = lipgloss.NewStyle().
            Border(lipgloss.RoundedBorder()).
            BorderForeground(colorMuted).
            Padding(0, 1)

    activePaneStyle = paneStyle.BorderForeground(colorPrimary)

    mutedStyle  = lipgloss.NewStyle().Foreground(colorMuted)
    dirtyStyle  = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
    selectedRow = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(colorAccent)
    helpStyle   = mutedStyle
)
```

A few things to absorb here.

**`var (...)` group.** Same as the `var (...)` in `cmd/task_create.go`
we saw earlier — just a grouped declaration. All these variables are
package-level, visible to any code in `package tui`.

**`lipgloss.Color("#5FAF87")` returns a value, not a string.**
`lipgloss.Color` is its own type that knows how to render across
different terminal colour profiles. That's why this works on a
truecolor terminal and degrades to ANSI 256 elsewhere — Lipgloss does
the conversion at render time.

**Method chaining as the API.**

```go
titleStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#ffffff")).
        Background(colorPrimary).
        Bold(true).
        Padding(0, 1)
```

`lipgloss.NewStyle()` returns a `lipgloss.Style` value. Each method
(`Foreground`, `Background`, `Bold`, `Padding`) returns a new
`lipgloss.Style` with the requested attribute set. You build up the
final style by chaining method calls.

This works because `lipgloss.Style` is **immutable** — every method
returns a copy of the style with the change applied. That means:

```go
activePaneStyle = paneStyle.BorderForeground(colorPrimary)
```

…doesn't mutate `paneStyle`. It produces a new style derived from
`paneStyle` with one attribute changed. `paneStyle` is unaffected.
This is a common Go idiom for "configuration objects you want to
reuse with small variations."

**`Padding(0, 1)`** is two positional args: vertical padding 0,
horizontal padding 1. So one space of padding on each side, zero
above/below. The two-arg form is one of several overloads — Lipgloss
also accepts `Padding(top, right, bottom, left)`.

To actually use a style:

```go
titleStyle.Render(" forest ")
```

`Render` takes a string, returns a string with ANSI escape codes
embedded. forest only ever calls `Render` from inside `View`. Nothing
about Lipgloss talks to the terminal directly — it just produces
strings. That's why it's safe to call from anywhere, including
non-TTY contexts (the escapes are still in there but they're harmless
when not interpreted).

## 2. Bubble Tea — the Elm Architecture in 60 seconds

Bubble Tea is a framework. To use it you need to understand its
contract. Once you do, the rest is detail.

A Bubble Tea program has three things:

1. **State**, held in a `Model`. Whatever struct you want.
2. **Messages** that change state. Anything implementing the
   `tea.Msg` interface (which is `interface{}`, i.e. anything).
3. **Three methods** on the `Model`:
   - `Init() tea.Cmd` — runs once at program start. Optionally
     returns a `tea.Cmd` (a side-effecting thing the runtime should
     do for you).
   - `Update(msg tea.Msg) (tea.Model, tea.Cmd)` — given a message,
     return a new model and optionally a command.
   - `View() string` — given the current model, return the screen as
     one big string. Pure function — no mutation, no I/O.

The runtime loop:

```
read event (key, resize, completed Cmd, ...)
        ↓
   call Update(currentModel, event)
        ↓
   adopt returned model as the new currentModel
        ↓
   call View(currentModel) to get screen text
        ↓
   diff against previous screen, write minimal updates to terminal
        ↓
   loop
```

The discipline: **`Update` is the only place state changes**. There
are no callbacks, no event listeners, no global subscriptions. Every
side-effecting action (timer, HTTP fetch, shell-out) goes through a
`tea.Cmd`, which the runtime invokes off the main loop and feeds the
result back as a message.

This is why Bubble Tea programs are easy to reason about: you can put
your finger on the model and trace every state change to a single
function.

## 3. forest's model

Open [`internal/tui/app.go`](../internal/tui/app.go).

```go
type model struct {
    p       *project.Project
    tasks   []workspace.Task
    cursor  int
    width   int
    height  int
    message string
}
```

| Field | Why it's there |
|-------|----------------|
| `p` | The forest project — needed to call `workspace.List`, `Remove`, `envfiles.Sync`. Same `*project.Project` that the CLI commands take. |
| `tasks` | Snapshot of all tasks loaded by `reload()`. Re-fetched when the user presses `r`. |
| `cursor` | Index into `tasks` of the highlighted row. Moves on `j`/`k`. |
| `width`, `height` | Terminal dimensions. Captured from `tea.WindowSizeMsg`. |
| `message` | One-line status / error to show. Empty string means "show the help line instead". |

Note the type is `model` (lowercase) — package-private. Nothing
outside `internal/tui` ever sees it. The only export from this
package is `Run(p *project.Project) error`.

Constructor:

```go
func newModel(p *project.Project) *model {
    m := &model{p: p}
    m.reload()
    return m
}
```

Standard Go shape: a `New<Type>` (or in this case `new<type>`)
constructor that returns a pointer to an initialised struct. forest
loads the task list immediately so the first `View` call has data —
this is why `Init()` can return `nil`.

```go
func (m *model) reload() {
    tasks, err := workspace.List(m.p)
    if err != nil {
        m.message = "list error: " + err.Error()
        return
    }
    m.tasks = tasks
    if m.cursor >= len(tasks) {
        m.cursor = 0
    }
}
```

Note the **pointer receiver** `(m *model)`. We need to mutate `m`
(set `m.tasks`, `m.cursor`, `m.message`), so the receiver must be a
pointer. If it were `(m model)` we'd be mutating a copy.

`reload` is called both from `newModel` (initial load) and from the
`r` key handler. The error-handling path stuffs the message into
`m.message` so the next `View` will display it.

## 4. Booting — `Run` and `Init`

```go
func Run(p *project.Project) error {
    m := newModel(p)
    prog := tea.NewProgram(m, tea.WithAltScreen())
    _, err := prog.Run()
    return err
}
```

Three lines, but three new things:

- **`tea.NewProgram(m, ...options)`** wraps the model into a
  `*tea.Program`. The variadic args are options — here we pass
  `tea.WithAltScreen()`, which tells the runtime to use the
  terminal's *alternate screen buffer*. This is the same trick `vim`
  and `less` use: when you exit, your shell's prior contents come
  back, instead of leaving the TUI's last frame on screen.
- **`prog.Run()`** blocks until the program quits (via `tea.Quit` or
  `Ctrl+C`) and returns `(tea.Model, error)`. We discard the final
  model with `_` — we don't need to inspect it.
- The function returns whatever error came back. `cmd/tui.go` then
  surfaces it through Cobra.

Then `Init`:

```go
func (m *model) Init() tea.Cmd { return nil }
```

forest has nothing to do at boot — `newModel` already loaded the
task list synchronously. So `Init` returns `nil`, meaning "no
command to run."

If we wanted to fetch tasks asynchronously (so the program could show
a "loading…" screen while git ran), we'd return a `tea.Cmd` here that
calls `workspace.List` off the main loop and yields a custom message.
That's covered later under "things forest doesn't do."

## 5. `Update` — every key, every resize

```go
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
    case tea.KeyMsg:
        switch msg.String() {
        case "q", "ctrl+c", "esc":
            return m, tea.Quit
        case "j", "down":
            if m.cursor < len(m.tasks)-1 {
                m.cursor++
            }
        case "k", "up":
            if m.cursor > 0 {
                m.cursor--
            }
        case "r":
            m.reload()
            m.message = "Reloaded"
        case "s":
            if t := m.current(); t != nil {
                results, err := envfiles.Sync(m.p.Root, t.Dir, m.p.Config.EnvFiles, false)
                if err != nil {
                    m.message = "env sync: " + err.Error()
                } else {
                    m.message = fmt.Sprintf("env sync: %d files processed (use CLI for --force)", len(results))
                }
            }
        case "d":
            if t := m.current(); t != nil {
                if err := workspace.Remove(m.p, t.Meta.Name, false, false); err != nil {
                    m.message = "remove refused: " + err.Error()
                } else {
                    m.message = "Removed " + t.Meta.Name
                    m.reload()
                }
            }
        }
    }
    return m, nil
}
```

This is the densest piece of new Go in the codebase. Three things to
break down.

### 5.1. The type switch

```go
switch msg := msg.(type) {
case tea.WindowSizeMsg:
    ...
case tea.KeyMsg:
    ...
}
```

`tea.Msg` is an interface (in fact `type Msg interface{}` — the
empty interface, satisfied by any value). To find out what kind of
message we got, we use a **type switch**.

`msg.(type)` is special syntax — it only works inside a type switch.
Each `case` matches a concrete type. Inside that case, `msg` is
re-bound to a typed value: in `case tea.KeyMsg`, `msg` has type
`tea.KeyMsg` and you can call its methods.

If none of the cases match, control just falls through. We don't have
a `default` clause; we silently ignore unknown messages. That's
fine — Bubble Tea routinely sends messages that we don't care about
(mouse events, focus changes, etc.).

### 5.2. `tea.KeyMsg.String()`

```go
case tea.KeyMsg:
    switch msg.String() {
    case "q", "ctrl+c", "esc":
        ...
```

`tea.KeyMsg` has a `String()` method that gives a normalised key
name: `"q"`, `"ctrl+c"`, `"shift+tab"`, `"up"`, `"enter"`, etc.

`case "q", "ctrl+c", "esc":` is a switch case with multiple values —
matches any of the three.

### 5.3. `tea.Quit` is a sentinel command

```go
return m, tea.Quit
```

`tea.Quit` is a function (`tea.Cmd`) that returns a special
`tea.QuitMsg`. The runtime recognises it and shuts down. We return
the same model `m` (we don't change anything) and `tea.Quit` as the
command.

For other key handlers, we mutate the model in place (because `m` is
a pointer receiver, this is fine) and `return m, nil`. No command,
nothing for the runtime to do other than re-render.

### 5.4. Why does `s` and `d` shell out synchronously?

Look at `case "s"` — it calls `envfiles.Sync` directly inside
`Update`. While that runs, the UI is blocked. For env sync that's
fine (it's milliseconds). For long-running operations (network calls,
big `git` operations) you'd be unhappy: keypresses would queue up
and the screen would freeze.

The "right" answer is to wrap the work in a `tea.Cmd`:

```go
func syncCmd(p *project.Project, t *workspace.Task) tea.Cmd {
    return func() tea.Msg {
        results, err := envfiles.Sync(p.Root, t.Dir, p.Config.EnvFiles, false)
        return syncDoneMsg{results: results, err: err}
    }
}
```

A `tea.Cmd` is a function that returns a `tea.Msg`. The runtime
invokes it on its own goroutine, then feeds the message back to
`Update`. You'd then add a `case syncDoneMsg:` in the type switch
and update `m.message` from there.

forest doesn't bother because all its operations are fast.

## 6. `View` — pure rendering

```go
func (m *model) View() string {
    if m.width == 0 {
        return "loading..."
    }
    left := m.viewList()
    right := m.viewDetails()

    leftPane := activePaneStyle.Width(m.width / 3).Height(m.height - 4).Render(left)
    rightPane := paneStyle.Width(m.width - m.width/3 - 4).Height(m.height - 4).Render(right)

    body := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

    header := titleStyle.Render(" forest ") + "  " + mutedStyle.Render(m.p.Root)
    help := helpStyle.Render("j/k: move • r: reload • s: env sync • d: remove • q: quit")
    status := m.message
    if status == "" {
        status = help
    }

    return lipgloss.JoinVertical(lipgloss.Left, header, body, status)
}
```

The `if m.width == 0` guard handles the very first frame: we haven't
yet received a `tea.WindowSizeMsg`, so we don't know the terminal
size. Returning a placeholder is the polite move.

**`lipgloss.JoinHorizontal` and `JoinVertical`** are the two layout
primitives you'll reach for 90% of the time:

```go
JoinHorizontal(align, parts...)  // parts side-by-side
JoinVertical(align, parts...)    // parts stacked
```

`align` is a `lipgloss.Position` — `lipgloss.Top` / `Center` /
`Bottom` for horizontal joins, `Left` / `Center` / `Right` for
vertical joins. It controls how parts of different lengths line up
along the perpendicular axis.

Forest uses this composition to build the screen:

```
┌─ titleStyle.Render(" forest ")   "  "  mutedStyle.Render(p.Root) ─┐  ← header
└────────────────────────────────────────────────────────────────────┘
┌─────────── activePaneStyle ──────┬────── paneStyle ──────────────┐
│  task list                       │  task details                  │  ← body (JoinHorizontal)
│  (m.viewList())                  │  (m.viewDetails())             │
└──────────────────────────────────┴────────────────────────────────┘
help or message                                                        ← status
```

Three lines stacked vertically (header, body, status), with the body
itself being two panes joined horizontally.

The width math:

```go
leftPane := activePaneStyle.Width(m.width / 3).Height(m.height - 4).Render(left)
rightPane := paneStyle.Width(m.width - m.width/3 - 4).Height(m.height - 4).Render(right)
```

- `m.width / 3` — left pane gets a third of the terminal.
- `m.width - m.width/3 - 4` — right pane gets the rest, minus 4 for
  borders / padding fudge.
- `m.height - 4` — leave room for header (1), body, status (1), gaps.

These integer fudges are the price of doing layout by hand. For
production UIs you'd usually use `bubbles/viewport` and
`bubbles/list` to handle scroll and sizing automatically.

The two view helpers are simple string builders:

```go
func (m *model) viewList() string {
    if len(m.tasks) == 0 {
        return mutedStyle.Render("No tasks. Use the CLI:\nforest task create <name>")
    }
    var b strings.Builder
    b.WriteString(titleStyle.Render(" tasks ") + "\n\n")
    for i, t := range m.tasks {
        line := fmt.Sprintf("%-20s  %d repos", t.Meta.Name, len(t.Meta.Repos))
        if i == m.cursor {
            b.WriteString(selectedRow.Render("▶ "+line) + "\n")
        } else {
            b.WriteString("  " + line + "\n")
        }
    }
    return b.String()
}
```

`strings.Builder` is the fast string-concatenation type. (`s := s +
"foo"` in a loop is quadratic; `b.WriteString` is linear.)

`%-20s` is `fmt`'s "left-pad the string to width 20". The `-`
left-justifies; without it you'd get right-justified.

`viewDetails` is the same shape — string the per-repo lines together
with `gitx.Stat` and `gitx.AheadBehind` calls inline. Those are
synchronous git shell-outs, but again, fast enough that nobody notices.

## 7. Wiring it up — what `cmd/tui.go` does

```go
var tuiCmd = &cobra.Command{
    Use:   "tui",
    Short: "Interactive task explorer",
    RunE: func(cmd *cobra.Command, args []string) error {
        p, err := project.Load(flagProjectRoot)
        if err != nil {
            return err
        }
        return tui.Run(p)
    },
}
```

Just a Cobra command that loads the project and calls `tui.Run`. The
`RunE` here is an inline anonymous function (a closure) — that's an
alternative style to forest's other commands which use named
functions like `runTaskCreate`. Both work; inline is shorter when
the body is two lines.

## 8. Huh — one-shot prompts using Bubble Tea internally

forest uses Huh in three places: `cmd/init.go` (multi-select for
which repos to track), `cmd/task_create.go` (multi-select for which
repos to include in a task), and `cmd/agent_start.go` (single-select
for which task to attach to).

The shape is always the same. From `cmd/agent_start.go`:

```go
opts := make([]huh.Option[string], len(tasks))
for i, t := range tasks {
    opts[i] = huh.NewOption(t.Meta.Name, t.Meta.Name)
}
var picked string
form := huh.NewForm(huh.NewGroup(
    huh.NewSelect[string]().
        Title("Pick a task").
        Options(opts...).
        Value(&picked),
))
if err := form.Run(); err != nil {
    return "", err
}
return picked, nil
```

Walk through it:

1. **Build options.** `huh.Option[string]` is a generic option whose
   value is `string`. The two args to `NewOption` are the *display
   label* and the *value*. They're often the same string but don't
   have to be (e.g. `huh.NewOption("Yes, delete it", "yes")`).
2. **Declare a destination.** `var picked string` — this is where
   the user's choice will land.
3. **Build the form.** `huh.NewForm(groups...)` takes one or more
   `huh.Group`s. Each group is one screen of the form. We build a
   single group with a single field — a `Select` with a title,
   options, and a destination.

   The `&picked` is critical: it's how the form knows where to
   write. Huh stores a pointer to your variable and writes to it
   when the user submits.
4. **`form.Run()`** internally constructs a Bubble Tea program and
   blocks until the user submits or cancels. After it returns,
   `picked` holds the choice.

For multi-select (`cmd/init.go`, `cmd/task_create.go`) the shape is
identical except `var picked []string` and `huh.NewMultiSelect`.

That's all there is to Huh in this codebase. Worth knowing the
library has more (text inputs, confirmation prompts, validators,
multi-page forms) — see its README for the full surface.

A subtlety: forest gates every Huh call behind `isTTY()` or
`--no-prompt`, because Huh draws to the terminal and would crash or
block in a CI pipe.

## 9. Things forest's TUI doesn't do (and could)

A short tour of idioms forest skips, useful for context:

### a) `tea.Cmd` for async work

forest does all its work synchronously inside `Update`. The idiomatic
move for slow operations is a `tea.Cmd`:

```go
type tasksLoadedMsg struct {
    tasks []workspace.Task
    err   error
}

func loadTasksCmd(p *project.Project) tea.Cmd {
    return func() tea.Msg {
        tasks, err := workspace.List(p)
        return tasksLoadedMsg{tasks: tasks, err: err}
    }
}
```

Then in `Init`:

```go
func (m *model) Init() tea.Cmd { return loadTasksCmd(m.p) }
```

And in `Update`:

```go
case tasksLoadedMsg:
    if msg.err != nil { m.message = msg.err.Error(); return m, nil }
    m.tasks = msg.tasks
    return m, nil
```

The runtime invokes `loadTasksCmd` on its own goroutine, then sends
the returned `tasksLoadedMsg` back through `Update`. You stay
consistent with the discipline ("state changes only in `Update`")
without blocking the UI.

### b) Bubbles components

The `github.com/charmbracelet/bubbles` package ships prefab
components: `list`, `table`, `viewport`, `textinput`, `spinner`,
`progress`. forest hand-rolls everything. A more grown-up version of
this TUI would:

- replace `viewList` with `bubbles/list.Model` (built-in scrolling,
  filtering, pagination),
- wrap `viewDetails` in `bubbles/viewport.Model` for scrolling on
  small terminals,
- show a `bubbles/spinner.Model` while async commands run.

To embed a Bubbles component, you store an instance on your model
and route relevant messages to it. Pattern:

```go
type model struct {
    list list.Model
    ...
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    m.list, cmd = m.list.Update(msg)  // delegate to the component
    return m, cmd
}
```

(Real implementations dispatch only the messages the component cares
about — full keypress handling depends on focus.)

### c) Focus model

forest's screen has only one selectable thing (the task list). If the
right pane became interactive (e.g. per-repo actions), you'd add a
`focus` field to the model and route keys based on which pane is
active.

## 10. Reading list

In rough order of "this teaches you the most for the least time":

1. The "Tutorial" in the [Bubble Tea README](https://github.com/charmbracelet/bubbletea)
   — the canonical "shopping list" example, ~50 lines.
2. The [`charmbracelet/bubbles`](https://github.com/charmbracelet/bubbles)
   prefab components — read at least the `list` and `viewport`
   READMEs.
3. The [Lipgloss README](https://github.com/charmbracelet/lipgloss) —
   one-liners with examples for every method.
4. The [Huh README](https://github.com/charmbracelet/huh) — forms
   beyond just select prompts.

Then come back to [`internal/tui/app.go`](../internal/tui/app.go) and
read it again. It should feel almost trivial.

## 11. Exercises

If you want to lock the framework in:

1. **Add a "Force Sync" key (`S`).** Same as `s` but with `force=true`.
   Show the result in `m.message`. Smallest possible change.
2. **Sort tasks by `CreatedAt` descending instead of by name.** This
   doesn't touch the framework, but it'll force you to read
   `workspace.List` and decide whether to sort there or in the TUI.
3. **Show a confirmation Huh form before `d` (remove).** Two ways to
   do this: the simple way is to set a `confirming bool` on the
   model and only delete on a second `d`-then-`y`. The Huh-native way
   is to spawn a child Huh form from inside `Update` — harder, more
   correct.
4. **Replace `viewList` with `bubbles/list.Model`.** Big refactor,
   but you'll learn how to embed a Bubbles component.
5. **Make `s` (env sync) async via a `tea.Cmd`.** Add a custom
   message type, route it through `Update`. Shows you the async path.

If you finish exercise 5, you've used roughly 80% of what
day-to-day Bubble Tea programs use. The rest is just more
components.
