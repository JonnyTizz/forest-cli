# 05 — Go TUIs with Bubble Tea, Lipgloss, and Huh

This doc is a Bubble Tea primer using forest's TUI as the worked example. By
the end you'll be able to read [`internal/tui/app.go`](../internal/tui/app.go)
and [`internal/tui/styles.go`](../internal/tui/styles.go) line-by-line and
know what every call does.

forest uses three Charm libraries:

| Library | What it does | Where forest uses it |
|---------|--------------|----------------------|
| **Bubble Tea** | Framework for full-screen interactive programs. The Elm Architecture in Go: a `Model`, a `View`, an `Update`. | The `forest tui` command. |
| **Lipgloss** | Style + layout for terminal output (colours, padding, borders, joins). | `internal/tui/styles.go` and the View. |
| **Huh** | Forms — single/multi-select prompts, text inputs — built on top of Bubble Tea. | The interactive prompts in `forest init`, `forest task create`, etc. |

You can use Lipgloss without Bubble Tea (just for styled `fmt.Println`) and
you can use Huh on its own (which is what forest does for one-off prompts).
Bubble Tea is the heaviest of the three; you only reach for it when you
want a long-lived screen with key bindings.

---

## The Elm Architecture in 60 seconds

Bubble Tea is built around three things:

1. **State** — a `Model` struct that holds everything you need to render.
2. **Messages** — events that change state (key presses, window resize,
   timer ticks, completed I/O, …).
3. **Functions** —
   - `Init() tea.Cmd` — runs once at startup, can return an initial command
     (e.g. "fetch this URL").
   - `Update(msg tea.Msg) (tea.Model, tea.Cmd)` — given a message, return a
     new model and optionally a new command to run.
   - `View() string` — given the current model, return the screen contents
     as a single string.

The runtime loop:

```
read event → Update(model, event) → new model → View(new model) → render
```

There is no mutation outside `Update`. There are no callbacks. Every
side-effecty action (HTTP request, file I/O, timer) goes through a `tea.Cmd`
which the runtime invokes off the main loop and feeds the result back as a
message.

This is why Bubble Tea programs are easy to reason about: the entire state
of the world is one struct, and the only place it changes is `Update`.

---

## forest's model

[`internal/tui/app.go`](../internal/tui/app.go):

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

| Field | Purpose |
|-------|---------|
| `p` | The loaded forest project — needed to call `workspace.List`, `Remove`, etc. |
| `tasks` | Snapshot loaded by `reload()`. Re-fetched on `r`. |
| `cursor` | Index into `tasks` of the currently highlighted row. |
| `width`, `height` | Terminal dimensions, captured from `tea.WindowSizeMsg`. |
| `message` | Ephemeral status line ("Reloaded", "remove refused: ...", or "" for "show help"). |

`newModel(p)` constructs and immediately `reload()`s. So the model has data
the moment Bubble Tea starts the program.

## `Run` — booting the program

```go
func Run(p *project.Project) error {
    m := newModel(p)
    prog := tea.NewProgram(m, tea.WithAltScreen())
    _, err := prog.Run()
    return err
}
```

Two things to notice:

1. `tea.WithAltScreen()` — uses the terminal's "alternate screen buffer"
   (like `vim`, `less`, `man`). Your shell's prior contents come back when
   the program exits.
2. `prog.Run()` blocks until the program quits and returns
   `(tea.Model, error)`. We discard the final model — we don't need it.

## `Init` — the first message

```go
func (m *model) Init() tea.Cmd { return nil }
```

forest has nothing to do at startup (the data is already in `tasks` from
`newModel`), so it returns `nil`. If we wanted to fetch network data on
boot we'd return a `tea.Cmd` that does the I/O and yields a custom message.

## `Update` — the heart of input handling

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
            if m.cursor < len(m.tasks)-1 { m.cursor++ }
        ...
```

Things to internalise:

- `msg` is `tea.Msg` (an empty interface). You type-switch to find out what
  it is. Two of the most common: `tea.WindowSizeMsg` (terminal resized) and
  `tea.KeyMsg` (key pressed).
- `msg.String()` on `tea.KeyMsg` gives a normalised key name:
  `"q"`, `"ctrl+c"`, `"shift+tab"`, `"up"`, `"enter"`, ...
- `tea.Quit` is a special command the runtime recognises as "stop the
  program."
- The function returns `(tea.Model, tea.Cmd)`. forest mutates `m` directly
  and returns `m, nil`. That works because `m` is a pointer receiver and the
  runtime treats the returned model as the new state.

The `s` and `d` cases call out to forest's `internal/envfiles` and
`internal/workspace` packages directly, **inside** `Update`. That's a
convenient simplification — these are fast, synchronous calls — but in
larger programs you'd push them off the main loop via `tea.Cmd`s so the UI
stays responsive. Here, the UI froze for at most a few ms while git ran;
fine for an explorer-style screen.

## `View` — pure rendering

`View` must be a pure function of the model. No side effects. It returns a
single string; the runtime is responsible for diffing it against the
previous frame and writing minimal updates to the terminal.

forest's view assembles three pieces:

```go
header := titleStyle.Render(" forest ") + "  " + mutedStyle.Render(m.p.Root)
body   := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
status := m.message
if status == "" { status = help }
return lipgloss.JoinVertical(lipgloss.Left, header, body, status)
```

`lipgloss.JoinHorizontal(align, parts...)` puts strings side by side,
aligning along the chosen axis. `JoinVertical` does the same but stacked.
These are the two layout primitives you'll use 90% of the time.

---

## Lipgloss styles, explained from forest's `styles.go`

```go
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
    ...
)
```

A `lipgloss.Style` is an immutable value built fluently. To use it,
`style.Render("text")` returns a string with the appropriate ANSI escape
sequences embedded. forest only ever calls `Render` inside `View`.

Patterns to notice:

- **Composition**. `activePaneStyle = paneStyle.BorderForeground(colorPrimary)`
  derives a new style from an existing one. Lipgloss is value-based, so this
  is a copy-with-tweaks.
- **Padding(v, h)**. Vertical / horizontal padding in cells.
- **Border(lipgloss.RoundedBorder())**. Lipgloss ships several preset
  borders (`NormalBorder`, `RoundedBorder`, `ThickBorder`, etc.). You can
  also define your own via `lipgloss.Border{...}`.

In `View`:

```go
leftPane := activePaneStyle.Width(m.width / 3).Height(m.height - 4).Render(left)
rightPane := paneStyle.Width(m.width - m.width/3 - 4).Height(m.height - 4).Render(right)
```

`Width` / `Height` set explicit dimensions; Lipgloss pads / truncates to fit.
The `-4` in the height accounts for the header line and the status line and
their gap; the `-4` in the right pane width leaves room for borders. These
fudge factors are the price of doing layout by hand — for production UIs
you'd typically use `bubbles/viewport` and friends to handle scrolling and
sizing for you.

---

## Things the forest TUI is **not** doing (yet)

Useful to know what's idiomatic Bubble Tea that this code skips:

- **No commands.** Everything in `Update` is synchronous. For longer-running
  things (network, shell-out) you'd typically write a function returning a
  `tea.Cmd`, e.g.:
  ```go
  func reloadCmd(p *project.Project) tea.Cmd {
      return func() tea.Msg {
          tasks, err := workspace.List(p)
          return tasksLoadedMsg{tasks: tasks, err: err}
      }
  }
  ```
  Then in `Update` you'd handle `tasksLoadedMsg` rather than calling
  `m.reload()` directly.
- **No Bubbles components.** The Charm `bubbles` package has prefab
  list/textinput/spinner/viewport components. A more "real" version of this
  TUI would use `bubbles/list` for the left pane and `bubbles/viewport` for
  the right pane (so it can scroll). Worth reading their READMEs if you want
  to extend the TUI.
- **No focus model.** There's only one selectable thing (the task list). If
  the right pane became interactive (e.g. per-repo actions), you'd add a
  `focus` field to the model and route keys based on which pane is active.

---

## Huh, briefly

Huh provides the multi-select / single-select prompts forest uses outside
the TUI — in `cmd/init.go`, `cmd/task_create.go`, `cmd/agent_start.go`. The
shape is always:

```go
opts := make([]huh.Option[string], len(items))
for i, x := range items {
    opts[i] = huh.NewOption(x, x).Selected(true) // .Selected for multi-pre-select
}
var chosen []string // or `var chosen string` for single-select
form := huh.NewForm(huh.NewGroup(
    huh.NewMultiSelect[string]().
        Title("…").
        Options(opts...).
        Value(&chosen),
))
if err := form.Run(); err != nil { return err }
```

`form.Run()` blocks, draws a Bubble Tea program internally, and returns
once the user submits or cancels. After it returns, the value pointed to by
`Value(&chosen)` holds the picks. forest uses `huh.NewSelect` for
single-pick (in `pickTask`) and `huh.NewMultiSelect` for multi-pick (in
init / task create).

This is why forest's CLI commands behave well in non-TTY mode: every Huh
call is gated by `isTTY()` or `--no-prompt` so they never try to draw on a
pipe.

---

## Reading list

In rough order:

1. The "Tutorial" in the [Bubble Tea README](https://github.com/charmbracelet/bubbletea)
   — the canonical "shopping list" example.
2. The [`charmbracelet/bubbles`](https://github.com/charmbracelet/bubbles)
   prefab components (list, table, viewport, textinput, spinner).
3. [Lipgloss README](https://github.com/charmbracelet/lipgloss) — every
   style method with examples.
4. [Huh README](https://github.com/charmbracelet/huh) — forms beyond just
   select prompts (text inputs, confirmation, validators).

Then come back to [`internal/tui/app.go`](../internal/tui/app.go) and read
it again — it should feel almost trivial.

## Exercises that'll teach you the framework

1. Add a "Force Sync" key (`S`) that runs env sync with `force=true` and
   shows the result in `m.message`.
2. Replace the hand-rolled list rendering in `viewList` with
   `bubbles/list.Model`. (You'll learn how to embed a Bubble component
   inside your own model.)
3. Make the right pane scroll: wrap its body in `bubbles/viewport.Model` and
   route `pgup`/`pgdn` to it.
4. Move the `s` (env sync) action onto a `tea.Cmd` so the UI stays
   responsive on a slow filesystem. You'll need a custom message type and a
   new `case` in `Update`.
5. Add a confirmation Huh form before `d` (remove). Bubble Tea programs can
   spawn child Huh forms via `huh.Form.Init/Update/View`, but the simplest
   approach is to set a `confirming bool` field on the model and only
   actually delete on a second `d`-then-`y` keypress.
