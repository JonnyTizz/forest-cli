package cmd

import "github.com/spf13/cobra"

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Launch AI coding agents inside a task workspace",
}

func init() {
	rootCmd.AddCommand(agentCmd)
}
