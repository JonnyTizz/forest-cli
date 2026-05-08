package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var trKeepBranches bool

var taskRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a task workspace (refuses on dirty worktrees unless --force)",
	Args:    cobra.ExactArgs(1),
	RunE:    runTaskRemove,
}

func init() {
	taskRemoveCmd.Flags().BoolVar(&trKeepBranches, "keep-branches", false, "Don't delete the task's branches after removing worktrees")
	taskCmd.AddCommand(taskRemoveCmd)
}

func runTaskRemove(cmd *cobra.Command, args []string) error {
	p, err := project.Load(flagProjectRoot)
	if err != nil {
		return err
	}
	if err := workspace.Remove(p, args[0], flagForce, trKeepBranches); err != nil {
		return err
	}
	fmt.Printf("Removed task %q\n", args[0])
	return nil
}
