package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagProjectRoot string
	flagForce       bool
	flagVerbose     bool
)

var rootCmd = &cobra.Command{
	Use:           "forest",
	Short:         "Manage multi-repo git worktree task workspaces",
	Long:          "forest creates per-task workspaces under .forest/worktrees/<task> with git worktrees of selected repos, copied env files, and generated agent context files.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagProjectRoot, "project-root", "", "Path to project root (default: walks up from CWD looking for .forest/)")
	rootCmd.PersistentFlags().BoolVar(&flagForce, "force", false, "Bypass safety checks (dirty worktrees, existing files, etc.)")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Verbose output")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// Force returns the global --force flag value.
func Force() bool { return flagForce }

// Verbose returns the global -v flag value.
func Verbose() bool { return flagVerbose }

// ProjectRootFlag returns the explicit --project-root if set.
func ProjectRootFlag() string { return flagProjectRoot }
