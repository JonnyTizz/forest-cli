package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/JonnyTizz/forest/internal/config"
)

// Project represents a resolved project root with its loaded config.
type Project struct {
	Root   string // absolute path to project root
	Config *config.ProjectConfig
}

// FindRoot walks up from start (or CWD if empty) looking for a .forest/ directory.
// If override is non-empty it is used directly without walking.
func FindRoot(override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		if !dirExists(filepath.Join(abs, config.Dirname)) {
			return "", fmt.Errorf("no %s/ directory at %s", config.Dirname, abs)
		}
		return abs, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if dirExists(filepath.Join(dir, config.Dirname)) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s/ found in %s or any parent (run `forest init` first)", config.Dirname, cwd)
		}
		dir = parent
	}
}

// Load resolves the project root and loads its config.
func Load(override string) (*Project, error) {
	root, err := FindRoot(override)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(filepath.Join(root, config.Dirname, config.ConfigFile))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return &Project{Root: root, Config: cfg}, nil
}

// ForestDir returns <root>/.forest
func (p *Project) ForestDir() string { return filepath.Join(p.Root, config.Dirname) }

// AgentsDir returns <root>/.forest/agents
func (p *Project) AgentsDir() string { return filepath.Join(p.ForestDir(), config.AgentsDir) }

// WorktreesDir returns the absolute path to the configured worktrees directory.
func (p *Project) WorktreesDir() string {
	d := p.Config.WorktreesDir
	if !filepath.IsAbs(d) {
		d = filepath.Join(p.Root, d)
	}
	return d
}

// TaskDir returns the absolute path to a task workspace.
func (p *Project) TaskDir(name string) string {
	return filepath.Join(p.WorktreesDir(), name)
}

// RepoPath returns the absolute path to a configured repo.
func (p *Project) RepoPath(name string) (string, error) {
	r, ok := p.Config.FindRepo(name)
	if !ok {
		return "", fmt.Errorf("repo %q not in config", name)
	}
	rp := r.Path
	if !filepath.IsAbs(rp) {
		rp = filepath.Join(p.Root, rp)
	}
	return rp, nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// ErrNoProject is returned when no project root can be located.
var ErrNoProject = errors.New("no forest project found")
