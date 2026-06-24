package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/config"
	"github.com/JonnyTizz/forest/internal/gitx"
)

const overviewTemplate = `# Project overview

This file is concatenated into every task's AGENTS.md / CLAUDE.md by ` + "`forest`" + `.
Add files like ` + "`10-frontend.md`" + ` next to this one — they are concatenated in
sorted filename order, so prefixed numbers control the ordering.

Replace this content with whatever context you want every AI agent to start
with for any task in this project.
`

var (
	initNoPrompt bool
	initRepos    []string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialise a forest project in the current directory",
	RunE:  runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initNoPrompt, "no-prompt", false, "Skip interactive prompts; use detected defaults")
	initCmd.Flags().StringSliceVar(&initRepos, "repos", nil, "Comma-separated list of repos to include (default: all detected git repos)")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	forestDir := filepath.Join(cwd, config.Dirname)
	if _, err := os.Stat(forestDir); err == nil && !flagForce {
		return fmt.Errorf("%s already exists (use --force to overwrite config)", forestDir)
	}

	detected, err := detectRepos(cwd)
	if err != nil {
		return err
	}
	if len(detected) == 0 {
		return errors.New("no git repositories found in immediate subdirectories — create or clone repos first, or run from the project root")
	}

	cfg := config.Defaults()
	selected := detected
	if len(initRepos) > 0 {
		selected = initRepos
	}

	if !initNoPrompt && len(initRepos) == 0 {
		var picked []string
		opts := make([]huh.Option[string], len(detected))
		for i, r := range detected {
			opts[i] = huh.NewOption(r, r).Selected(true)
		}
		form := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Which repos should this forest project track?").
				Options(opts...).
				Value(&picked),
		))
		if err := form.Run(); err != nil {
			return err
		}
		if len(picked) == 0 {
			return errors.New("no repos selected")
		}
		selected = picked
	}

	for _, name := range selected {
		base := "main"
		repoDir := filepath.Join(cwd, name)
		if gitx.IsRepo(repoDir) {
			base = gitx.DefaultBranch(repoDir)
		}
		cfg.Repos = append(cfg.Repos, config.Repo{
			Name:        name,
			Path:        name,
			DefaultBase: base,
		})
	}

	if err := os.MkdirAll(filepath.Join(forestDir, config.AgentsDir), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(cwd, cfg.WorktreesDir), 0o755); err != nil {
		return err
	}
	overview := filepath.Join(forestDir, config.AgentsDir, "00-overview.md")
	if _, err := os.Stat(overview); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(overview, []byte(overviewTemplate), 0o644); err != nil {
			return err
		}
	}
	if err := config.Save(filepath.Join(forestDir, config.ConfigFile), &cfg); err != nil {
		return err
	}

	fmt.Printf("Initialised forest project at %s\n", forestDir)
	fmt.Printf("Tracked repos: %s\n", strings.Join(selected, ", "))
	fmt.Printf("Edit %s and the agents fragments under %s/%s\n",
		filepath.Join(forestDir, config.ConfigFile),
		config.Dirname, config.AgentsDir)
	return nil
}

func detectRepos(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), ".git")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
