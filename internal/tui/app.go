package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JonnyTizz/forest/internal/envfiles"
	"github.com/JonnyTizz/forest/internal/gitx"
	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/workspace"
)

// Run boots the Bubble Tea TUI for the given project.
func Run(p *project.Project) error {
	m := newModel(p)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	_, err := prog.Run()
	return err
}

type model struct {
	p          *project.Project
	tasks      []workspace.Task
	cursor     int
	width      int
	height     int
	message    string // ephemeral status line
	confirmDel bool   // awaiting y/n confirmation for a delete
}

func newModel(p *project.Project) *model {
	m := &model{p: p}
	m.reload()
	return m
}

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

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		// When a delete is pending, the next keypress is the confirmation.
		if m.confirmDel {
			switch msg.String() {
			case "y", "Y":
				m.confirmDel = false
				if t := m.current(); t != nil {
					warnings, err := workspace.Remove(m.p, t.Meta.Name, false, false)
					switch {
					case err != nil:
						m.message = "remove refused: " + err.Error()
					case len(warnings) > 0:
						m.message = fmt.Sprintf("Removed %s (with warnings: %s)", t.Meta.Name, strings.Join(warnings, "; "))
						m.reload()
					default:
						m.message = "Removed " + t.Meta.Name
						m.reload()
					}
				}
			default:
				m.confirmDel = false
				m.message = "Delete cancelled"
			}
			return m, nil
		}
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
				m.confirmDel = true
				m.message = dirtyStyle.Render(fmt.Sprintf("Delete task %q and its branches? (y/N)", t.Meta.Name))
			}
		}
	}
	return m, nil
}

func (m *model) current() *workspace.Task {
	if len(m.tasks) == 0 || m.cursor >= len(m.tasks) {
		return nil
	}
	return &m.tasks[m.cursor]
}

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
	help := helpStyle.Render("j/k: move • r: reload • s: env sync • d: remove (confirm) • q: quit")
	status := m.message
	if status == "" {
		status = help
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, body, status)
}

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

func (m *model) viewDetails() string {
	t := m.current()
	if t == nil {
		return mutedStyle.Render("no task selected")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\n",
		titleStyle.Render(" "+t.Meta.Name+" "),
		mutedStyle.Render(t.Dir))
	fmt.Fprintf(&b, "Created: %s\n\n", t.Meta.CreatedAt.Format("2006-01-02 15:04"))
	for _, r := range t.Meta.Repos {
		wt := filepath.Join(t.Dir, r.Name)
		s, err := gitx.Stat(wt)
		state := "?"
		if err == nil {
			if s.Clean {
				state = "clean"
			} else {
				state = dirtyStyle.Render(fmt.Sprintf("M%d S%d U%d", s.Modified, s.Staged, s.Untracked))
			}
		}
		ahead, behind, _ := gitx.AheadBehind(wt, r.Base)
		fmt.Fprintf(&b, "• %s @ %s  (base %s, +%d/-%d)  %s\n",
			r.Name, r.Branch, r.Base, ahead, behind, state)
	}
	return b.String()
}
