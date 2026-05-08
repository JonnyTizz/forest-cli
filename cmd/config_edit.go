package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/runner"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open a config target in $EDITOR",
}

var configEditAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Open .forest/agents/ in $EDITOR; regenerate AGENTS.md/CLAUDE.md for every task on exit",
	RunE:  runConfigEditAgents,
}

func init() {
	configCmd.AddCommand(configEditCmd)
	configEditCmd.AddCommand(configEditAgentsCmd)
}

func runConfigEditAgents(cmd *cobra.Command, args []string) error {
	p, err := project.Load(flagProjectRoot)
	if err != nil {
		return err
	}
	editor := runner.EditorCommand()
	// Use `sh -c` so $EDITOR can contain arguments (e.g. "code --wait", "vim -c X").
	c := exec.Command("sh", "-c", editor+` "$@"`, "sh", p.AgentsDir())
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor %q exited: %w", editor, err)
	}
	if err := workspace.RegenerateAllAgents(p); err != nil {
		return err
	}
	fmt.Println("Regenerated AGENTS.md / CLAUDE.md for all tasks.")
	return nil
}
