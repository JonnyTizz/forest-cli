package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/gitx"
	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var doctorFix bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check for and optionally repair inconsistent forest state",
	Long: "doctor reports problems that can accumulate over time:\n" +
		"  - orphaned task directories (no .forest-task.yaml metadata)\n" +
		"  - stale git worktree entries that point at task workspaces\n\n" +
		"Run with --fix to remove orphaned directories and prune stale worktree\n" +
		"entries from each configured repo.",
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Repair the problems found (remove orphan dirs, prune worktrees)")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	p, err := project.Load(flagProjectRoot)
	if err != nil {
		return err
	}

	problems := 0

	// 1. Orphaned task directories.
	orphans, err := workspace.Orphans(p)
	if err != nil {
		return err
	}
	for _, dir := range orphans {
		problems++
		if doctorFix {
			if err := os.RemoveAll(dir); err != nil {
				fmt.Printf("orphan %s: remove failed: %v\n", dir, err)
			} else {
				fmt.Printf("orphan %s: removed\n", dir)
			}
		} else {
			fmt.Printf("orphan %s: no task metadata (run with --fix to remove)\n", dir)
		}
	}

	// 2. Stale git worktree entries pointing into the worktrees dir.
	wtRoot := p.WorktreesDir()
	for _, r := range p.Config.Repos {
		repoPath, err := p.RepoPath(r.Name)
		if err != nil {
			continue
		}
		wts, err := gitx.Worktrees(repoPath)
		if err != nil {
			fmt.Printf("repo %s: cannot list worktrees: %v\n", r.Name, err)
			continue
		}
		stale := false
		for _, wt := range wts {
			underForest := strings.HasPrefix(wt.Path, wtRoot+string(os.PathSeparator))
			missing := wt.Prunable
			if _, statErr := os.Stat(wt.Path); statErr != nil {
				missing = true
			}
			if underForest && missing {
				problems++
				stale = true
				fmt.Printf("repo %s: stale worktree entry %s\n", r.Name, wt.Path)
			}
		}
		if stale && doctorFix {
			if err := gitx.WorktreePrune(repoPath); err != nil {
				fmt.Printf("repo %s: prune failed: %v\n", r.Name, err)
			} else {
				fmt.Printf("repo %s: pruned stale worktree entries\n", r.Name)
			}
		}
	}

	if problems == 0 {
		fmt.Println("No problems found.")
		return nil
	}
	if !doctorFix {
		return &ExitCodeError{Code: 2, Err: fmt.Errorf("%d problem(s) found; rerun with --fix to repair", problems)}
	}
	return nil
}
