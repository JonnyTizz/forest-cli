package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/runner"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var eoEditor string

var editorOpenCmd = &cobra.Command{
	Use:   "open [<task>]",
	Short: "Open the configured editor in a task workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runEditorOpen,
}

func init() {
	editorOpenCmd.Flags().StringVar(&eoEditor, "editor", "", "Editor name from config.editors.options (default: configured default)")
	editorCmd.AddCommand(editorOpenCmd)
}

func runEditorOpen(cmd *cobra.Command, args []string) error {
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
	spec, name, err := runner.Resolve(p.Config.Editors, eoEditor)
	if err != nil {
		return err
	}
	fmt.Printf("Opening %q in %s\n", name, t.Dir)
	return runner.Spawn(t.Dir, spec, nil)
}
