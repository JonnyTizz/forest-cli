package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/gitx"
	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var taskStatusCmd = &cobra.Command{
	Use:   "status [<name>]",
	Short: "Show worktree status for a task (or all tasks)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTaskStatus,
}

func init() {
	taskCmd.AddCommand(taskStatusCmd)
}

func runTaskStatus(cmd *cobra.Command, args []string) error {
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
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TASK\tREPO\tBRANCH\tAHEAD/BEHIND\tDIRTY\tLAST")
	for _, t := range tasks {
		for _, r := range t.Meta.Repos {
			wt := filepath.Join(t.Dir, r.Name)
			s, err := gitx.Stat(wt)
			if err != nil {
				fmt.Fprintf(tw, "%s\t%s\t?\t?\t?\t%v\n", t.Meta.Name, r.Name, err)
				continue
			}
			ahead, behind, _ := gitx.AheadBehind(wt, r.Base)
			dirty := "clean"
			if !s.Clean {
				dirty = fmt.Sprintf("M%d S%d U%d", s.Modified, s.Staged, s.Untracked)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t+%d/-%d\t%s\t%s\n",
				t.Meta.Name, r.Name, s.Branch, ahead, behind, dirty, truncate(s.LastCommit, 50))
		}
	}
	return tw.Flush()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
