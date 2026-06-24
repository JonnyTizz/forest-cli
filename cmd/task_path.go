package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var taskPathCmd = &cobra.Command{
	Use:   "path <name> [<repo>]",
	Short: "Print the on-disk path of a task workspace (or one repo's worktree)",
	Long: "Print the absolute path to a task workspace so it can be used with cd, e.g.\n" +
		"  cd \"$(forest task path my-task)\"\n" +
		"With a repo argument, prints that repo's worktree path inside the task.",
	Args: cobra.RangeArgs(1, 2),
	RunE: runTaskPath,
}

func init() {
	taskCmd.AddCommand(taskPathCmd)
}

func runTaskPath(cmd *cobra.Command, args []string) error {
	p, err := project.Load(flagProjectRoot)
	if err != nil {
		return err
	}
	t, err := workspace.Get(p, args[0])
	if err != nil {
		return err
	}
	if len(args) == 2 {
		repo := args[1]
		found := false
		for _, r := range t.Meta.Repos {
			if r.Name == repo {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("repo %q is not part of task %q", repo, t.Meta.Name)
		}
		fmt.Println(filepath.Join(t.Dir, repo))
		return nil
	}
	fmt.Println(t.Dir)
	return nil
}
