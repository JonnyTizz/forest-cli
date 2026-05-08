package cmd

import "github.com/spf13/cobra"

var editorCmd = &cobra.Command{
	Use:   "editor",
	Short: "Open editors inside a task workspace",
}

func init() {
	rootCmd.AddCommand(editorCmd)
}
