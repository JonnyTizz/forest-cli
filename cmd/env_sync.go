package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/envfiles"
	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var envSyncCmd = &cobra.Command{
	Use:   "sync [<task>]",
	Short: "Copy configured env files from project root into task workspaces",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runEnvSync,
}

func init() {
	envCmd.AddCommand(envSyncCmd)
}

func runEnvSync(cmd *cobra.Command, args []string) error {
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
	if len(tasks) == 0 {
		return errors.New("no tasks to sync")
	}

	var refused int
	for _, t := range tasks {
		results, err := envfiles.Sync(p.Root, t.Dir, p.Config.EnvFiles, flagForce)
		if err != nil {
			return err
		}
		for _, r := range results {
			if r.Err != nil {
				fmt.Printf("[%s] %s -> %s: error: %v\n", t.Meta.Name, r.Source, r.Dest, r.Err)
				continue
			}
			fmt.Printf("[%s] %s -> %s: %s\n", t.Meta.Name, r.Source, r.Dest, r.Action)
			if r.Action == "refused-modified" {
				refused++
			}
		}
	}
	if refused > 0 && !flagForce {
		return fmt.Errorf("%d destination file(s) refused — they have local edits; re-run with --force to overwrite", refused)
	}
	return nil
}
