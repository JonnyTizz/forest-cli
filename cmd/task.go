package cmd

import "github.com/spf13/cobra"

var taskCmd = &cobra.Command{
	Use:     "task",
	Short:   "Manage task workspaces",
	Aliases: []string{"tasks"},
}

func init() {
	rootCmd.AddCommand(taskCmd)
}
