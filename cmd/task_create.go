package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var (
	tcRepos    []string
	tcBranch   string
	tcBase     string
	tcNoPrompt bool
)

var taskCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new task workspace",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskCreate,
}

func init() {
	taskCreateCmd.Flags().StringSliceVar(&tcRepos, "repos", nil, "Comma-separated repo names to include (default: prompt)")
	taskCreateCmd.Flags().StringVar(&tcBranch, "branch", "", "Branch name to create or check out (default: task/<name>)")
	taskCreateCmd.Flags().StringVar(&tcBase, "base", "", "Base branch to fork from (default: per-repo default_base)")
	taskCreateCmd.Flags().BoolVar(&tcNoPrompt, "no-prompt", false, "Fail rather than prompting for missing options")
	taskCmd.AddCommand(taskCreateCmd)
}

func runTaskCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := workspace.ValidateName(name); err != nil {
		return err
	}
	p, err := project.Load(flagProjectRoot)
	if err != nil {
		return err
	}

	repos := tcRepos
	if len(repos) == 0 {
		if tcNoPrompt || !isTTY() {
			return errors.New("--repos is required in non-interactive mode")
		}
		all := workspace.SuggestRepoOrder(p)
		if len(all) == 0 {
			return errors.New("no configured repos exist on disk")
		}
		opts := make([]huh.Option[string], len(all))
		for i, r := range all {
			opts[i] = huh.NewOption(r, r).Selected(true)
		}
		var picked []string
		form := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title(fmt.Sprintf("Which repos should task %q include?", name)).
				Options(opts...).
				Value(&picked),
		))
		if err := form.Run(); err != nil {
			return err
		}
		if len(picked) == 0 {
			return errors.New("no repos selected")
		}
		repos = picked
	}

	t, err := workspace.Create(p, workspace.CreateOptions{
		Name:   name,
		Repos:  repos,
		Branch: tcBranch,
		Base:   tcBase,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Created task %q at %s\n", t.Meta.Name, t.Dir)
	for _, r := range t.Meta.Repos {
		fmt.Printf("  %s @ %s (from %s)\n", r.Name, r.Branch, r.Base)
	}
	return nil
}

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
