package cmd

import "github.com/spf13/cobra"

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Sync and inspect env files copied into task workspaces",
}

func init() {
	rootCmd.AddCommand(envCmd)
}
