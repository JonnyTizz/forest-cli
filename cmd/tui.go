package cmd

import (
	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactive task explorer",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := project.Load(flagProjectRoot)
		if err != nil {
			return err
		}
		return tui.Run(p)
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
