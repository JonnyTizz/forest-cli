package cmd

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/runner"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var asAgent string

var agentStartCmd = &cobra.Command{
	Use:   "start [<task>]",
	Short: "chdir into a task workspace and exec the configured AI agent",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAgentStart,
}

func init() {
	agentStartCmd.Flags().StringVar(&asAgent, "agent", "", "Agent name from config.agents.options (default: configured default)")
	agentCmd.AddCommand(agentStartCmd)
}

func runAgentStart(cmd *cobra.Command, args []string) error {
	p, err := project.Load(flagProjectRoot)
	if err != nil {
		return err
	}
	taskName, err := pickTask(p, args)
	if err != nil {
		return err
	}
	t, err := workspace.Get(p, taskName)
	if err != nil {
		return err
	}
	spec, name, err := runner.Resolve(p.Config.Agents, asAgent)
	if err != nil {
		return err
	}
	fmt.Printf("Starting agent %q in %s\n", name, t.Dir)
	return runner.ExecReplace(t.Dir, spec, nil)
}

// pickTask resolves a task name from args or, if missing and TTY, prompts the
// user with a Huh single-select.
func pickTask(p *project.Project, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	tasks, err := workspace.List(p)
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "", errors.New("no tasks exist; create one with `forest task create <name>`")
	}
	if !isTTY() {
		return "", errors.New("task name required in non-interactive mode")
	}
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
}
