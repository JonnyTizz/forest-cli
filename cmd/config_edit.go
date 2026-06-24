package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/config"
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

var configEditConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Open .forest/config.yaml in $EDITOR; validate it on exit",
	RunE:  runConfigEditConfig,
}

func init() {
	configCmd.AddCommand(configEditCmd)
	configEditCmd.AddCommand(configEditAgentsCmd)
	configEditCmd.AddCommand(configEditConfigCmd)
}

// runEditor launches $EDITOR (or $VISUAL, then vi) on target. The editor string
// is split into command + args with shell-style word splitting so values like
// "code --wait" work, but the target path is passed as a separate argv element
// rather than interpolated into a shell string — so a path or env value can
// never inject extra commands.
func runEditor(target string) error {
	editor := runner.EditorCommand()
	parts, err := runner.SplitArgs(editor)
	if err != nil {
		return fmt.Errorf("invalid editor command %q: %w", editor, err)
	}
	if len(parts) == 0 {
		return fmt.Errorf("empty editor command")
	}
	argv := append(parts[1:], target)
	c := exec.Command(parts[0], argv...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor %q exited: %w", editor, err)
	}
	return nil
}

func runConfigEditAgents(cmd *cobra.Command, args []string) error {
	p, err := project.Load(flagProjectRoot)
	if err != nil {
		return err
	}
	if err := runEditor(p.AgentsDir()); err != nil {
		return err
	}
	if err := workspace.RegenerateAllAgents(p); err != nil {
		return err
	}
	fmt.Println("Regenerated AGENTS.md / CLAUDE.md for all tasks.")
	return nil
}

func runConfigEditConfig(cmd *cobra.Command, args []string) error {
	p, err := project.Load(flagProjectRoot)
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(p.ForestDir(), config.ConfigFile)
	if err := runEditor(cfgPath); err != nil {
		return err
	}
	// Re-load to validate the edited config; report problems but leave the file
	// in place so the user can fix it.
	if _, err := config.Load(cfgPath); err != nil {
		return fmt.Errorf("config is invalid after editing: %w", err)
	}
	fmt.Println("Config OK.")
	return nil
}
