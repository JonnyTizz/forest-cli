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

	activePaneStyle = paneStyle.
			BorderForeground(colorPrimary)

	mutedStyle  = lipgloss.NewStyle().Foreground(colorMuted)
	dirtyStyle  = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
	selectedRow = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(colorAccent)
	helpStyle   = mutedStyle
)
