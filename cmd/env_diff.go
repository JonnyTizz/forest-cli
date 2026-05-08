package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/envfiles"
	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var envDiffCmd = &cobra.Command{
	Use:   "diff [<task>]",
	Short: "Show drift between project-root env files and their copies in task workspaces",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runEnvDiff,
}

func init() {
	envCmd.AddCommand(envDiffCmd)
}

func runEnvDiff(cmd *cobra.Command, args []string) error {
	p, err := project.Load(flagProjectRoot)
	if err != nil {
		return err
	}
	var tasks []workspace.Task
	if len(args) == 1 {
		t, err := workspace.Get(p, args[0])
		if err != nil {
			return err
		}
		tasks = []workspace.Task{*t}
	} else {
		tasks, err = workspace.List(p)
		if err != nil {
			return err
		}
	}

	anyDrift := false
	for _, t := range tasks {
		entries, err := envfiles.Diff(p.Root, t.Dir, p.Config.EnvFiles)
		if err != nil {
			return err
		}
		for _, e := range entries {
			fmt.Printf("[%s] %s ↔ %s: %s\n", t.Meta.Name, e.Source, e.Dest, e.State)
			if e.State == "drift" {
				fmt.Print(e.Diff)
				anyDrift = true
			}
		}
	}
	if anyDrift {
		os.Exit(2)
	}
	return nil
}
