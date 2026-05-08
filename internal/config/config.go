package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	Dirname                = ".forest"
	ConfigFile             = "config.yaml"
	AgentsDir              = "agents"
	DefaultWorktreesSubdir = "worktrees"
)

type Repo struct {
	Name        string `yaml:"name"`
	Path        string `yaml:"path"`
	DefaultBase string `yaml:"default_base"`
}

type EnvFile struct {
	Source string `yaml:"source"`
	Dest   string `yaml:"dest"`
}

type CommandSpec struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args,omitempty"`
}

type CommandSet struct {
	Default string                 `yaml:"default"`
	Options map[string]CommandSpec `yaml:"options"`
}

type ProjectConfig struct {
	Version      int        `yaml:"version"`
	Repos        []Repo     `yaml:"repos"`
	EnvFiles     []EnvFile  `yaml:"env_files,omitempty"`
	Agents       CommandSet `yaml:"agents"`
	Editors      CommandSet `yaml:"editors"`
	WorktreesDir string     `yaml:"worktrees_dir,omitempty"`
}

func Defaults() ProjectConfig {
	return ProjectConfig{
		Version:      1,
		Repos:        nil,
		EnvFiles:     nil,
		WorktreesDir: filepath.Join(Dirname, DefaultWorktreesSubdir),
		Agents: CommandSet{
			Default: "claude",
			Options: map[string]CommandSpec{
				"claude": {Command: "claude"},
				"aider":  {Command: "aider"},
			},
		},
		Editors: CommandSet{
			Default: "cursor",
			Options: map[string]CommandSpec{
				"cursor": {Command: "cursor", Args: []string{"."}},
				"code":   {Command: "code", Args: []string{"."}},
			},
		},
	}
}

func Load(path string) (*ProjectConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c ProjectConfig
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.WorktreesDir == "" {
		c.WorktreesDir = filepath.Join(Dirname, DefaultWorktreesSubdir)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func Save(path string, c *ProjectConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (c *ProjectConfig) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d (expected 1)", c.Version)
	}
	if len(c.Repos) == 0 {
		return errors.New("config has no repos")
	}
	seen := map[string]bool{}
	for i, r := range c.Repos {
		if r.Name == "" {
			return fmt.Errorf("repos[%d]: name is required", i)
		}
		if strings.ContainsAny(r.Name, "/\\ ") {
			return fmt.Errorf("repos[%d]: name %q must not contain slashes or spaces", i, r.Name)
		}
		if seen[r.Name] {
			return fmt.Errorf("duplicate repo name %q", r.Name)
		}
		seen[r.Name] = true
		if r.Path == "" {
			return fmt.Errorf("repos[%d] (%s): path is required", i, r.Name)
		}
		if r.DefaultBase == "" {
			c.Repos[i].DefaultBase = "main"
		}
	}
	if c.Agents.Default != "" {
		if _, ok := c.Agents.Options[c.Agents.Default]; !ok {
			return fmt.Errorf("agents.default %q not in agents.options", c.Agents.Default)
		}
	}
	if c.Editors.Default != "" {
		if _, ok := c.Editors.Options[c.Editors.Default]; !ok {
			return fmt.Errorf("editors.default %q not in editors.options", c.Editors.Default)
		}
	}
	return nil
}

// Repo lookup by name.
func (c *ProjectConfig) FindRepo(name string) (Repo, bool) {
	for _, r := range c.Repos {
		if r.Name == name {
			return r, true
		}
	}
	return Repo{}, false
}

// RepoNames returns repo names in declaration order.
func (c *ProjectConfig) RepoNames() []string {
	out := make([]string, len(c.Repos))
	for i, r := range c.Repos {
		out[i] = r.Name
	}
	return out
}

// SortedRepoNames returns repo names alphabetically.
func (c *ProjectConfig) SortedRepoNames() []string {
	out := c.RepoNames()
	sort.Strings(out)
	return out
}
