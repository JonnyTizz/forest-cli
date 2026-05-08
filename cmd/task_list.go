package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/JonnyTizz/forest/internal/gitx"
	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/workspace"
)

var tlJSON bool

var taskListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List task workspaces",
	RunE:    runTaskList,
}

func init() {
	taskListCmd.Flags().BoolVar(&tlJSON, "json", false, "Emit machine-readable JSON")
	taskCmd.AddCommand(taskListCmd)
}

type listEntry struct {
	Name     string   `json:"name"`
	Dir      string   `json:"dir"`
	Repos    []string `json:"repos"`
	Branches []string `json:"branches"`
	Dirty    bool     `json:"dirty"`
	Created  string   `json:"created"`
}

func runTaskList(cmd *cobra.Command, args []string) error {
	p, err := project.Load(flagProjectRoot)
	if err != nil {
		return err
	}
	tasks, err := workspace.List(p)
	if err != nil {
		return err
	}

	entries := make([]listEntry, 0, len(tasks))
	for _, t := range tasks {
		e := listEntry{
			Name:    t.Meta.Name,
			Dir:     t.Dir,
			Created: t.Meta.CreatedAt.Format("2006-01-02"),
		}
		for _, r := range t.Meta.Repos {
			e.Repos = append(e.Repos, r.Name)
			e.Branches = append(e.Branches, r.Branch)
			wt := filepath.Join(t.Dir, r.Name)
			if s, err := gitx.Stat(wt); err == nil && !s.Clean {
				e.Dirty = true
			}
		}
		entries = append(entries, e)
	}

	if tlJSON {
		return json.NewEncoder(os.Stdout).Encode(entries)
	}
	if len(entries) == 0 {
		fmt.Println("No tasks. Create one with `forest task create <name>`.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tREPOS\tBRANCHES\tDIRTY\tCREATED")
	for _, e := range entries {
		dirty := ""
		if e.Dirty {
			dirty = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			e.Name,
			strings.Join(e.Repos, ","),
			strings.Join(e.Branches, ","),
			dirty,
			e.Created,
		)
	}
	return tw.Flush()
}
