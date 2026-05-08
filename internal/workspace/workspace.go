package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/JonnyTizz/forest/internal/agents"
	"github.com/JonnyTizz/forest/internal/envfiles"
	"github.com/JonnyTizz/forest/internal/gitx"
	"github.com/JonnyTizz/forest/internal/project"
	"github.com/JonnyTizz/forest/internal/safety"
)

const metaFile = ".forest-task.yaml"

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// RepoMeta records what was created for one repo in a task.
type RepoMeta struct {
	Name   string `yaml:"name"`
	Branch string `yaml:"branch"`
	Base   string `yaml:"base"`
}

// Meta is persisted as <task>/.forest-task.yaml.
type Meta struct {
	Name      string     `yaml:"name"`
	Repos     []RepoMeta `yaml:"repos"`
	CreatedAt time.Time  `yaml:"created_at"`
}

// Task is a hydrated workspace with its meta + on-disk path.
type Task struct {
	Meta Meta
	Dir  string
}

// CreateOptions controls task creation.
type CreateOptions struct {
	Name   string
	Repos  []string // names from config.Repos
	Branch string   // empty -> "task/<name>"; if it already exists, the worktree checks it out
	Base   string   // empty -> repo's default_base
}

// ValidateName ensures the task name is filesystem- and branch-safe.
func ValidateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid task name %q (use lowercase, digits, '.', '_', '-'; max 64)", name)
	}
	return nil
}

// Create materialises a new task workspace.
func Create(p *project.Project, opts CreateOptions) (*Task, error) {
	if err := ValidateName(opts.Name); err != nil {
		return nil, err
	}
	if len(opts.Repos) == 0 {
		return nil, errors.New("at least one repo must be selected")
	}
	taskDir := p.TaskDir(opts.Name)
	if _, err := os.Stat(taskDir); err == nil {
		return nil, fmt.Errorf("task %q already exists at %s", opts.Name, taskDir)
	}
	branch := opts.Branch
	if branch == "" {
		branch = "task/" + opts.Name
	}

	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return nil, err
	}

	meta := Meta{Name: opts.Name, CreatedAt: time.Now()}
	created := make([]string, 0, len(opts.Repos)) // absolute worktree paths, for rollback

	for _, repoName := range opts.Repos {
		repoCfg, ok := p.Config.FindRepo(repoName)
		if !ok {
			rollback(p, created, branch)
			os.RemoveAll(taskDir)
			return nil, fmt.Errorf("repo %q not configured", repoName)
		}
		repoPath, err := p.RepoPath(repoName)
		if err != nil {
			rollback(p, created, branch)
			os.RemoveAll(taskDir)
			return nil, err
		}
		base := opts.Base
		if base == "" {
			base = repoCfg.DefaultBase
		}
		wtPath := filepath.Join(taskDir, repoName)
		newBranch := branch
		if gitx.BranchExists(repoPath, branch) {
			newBranch = "" // check out existing
		}
		if err := gitx.WorktreeAdd(repoPath, wtPath, newBranch, base); err != nil {
			rollback(p, created, branch)
			os.RemoveAll(taskDir)
			return nil, fmt.Errorf("create worktree for %s: %w", repoName, err)
		}
		created = append(created, wtPath)
		meta.Repos = append(meta.Repos, RepoMeta{Name: repoName, Branch: branch, Base: base})
	}

	if err := writeMeta(taskDir, meta); err != nil {
		return nil, err
	}
	if err := RegenerateAgents(p, &meta, taskDir); err != nil {
		return nil, fmt.Errorf("write agents files: %w", err)
	}
	if _, err := envfiles.Sync(p.Root, taskDir, p.Config.EnvFiles, false); err != nil {
		return nil, fmt.Errorf("sync env files: %w", err)
	}
	return &Task{Meta: meta, Dir: taskDir}, nil
}

// Remove tears down a task workspace.
func Remove(p *project.Project, name string, force, keepBranches bool) error {
	t, err := Get(p, name)
	if err != nil {
		return err
	}

	// Safety pass: refuse if any worktree dirty / has unpushed.
	if !force {
		for _, r := range t.Meta.Repos {
			wt := filepath.Join(t.Dir, r.Name)
			if err := safety.EnsureClean(wt); err != nil {
				return err
			}
			if err := safety.EnsureNoUnpushed(wt); err != nil {
				return err
			}
		}
	}

	for _, r := range t.Meta.Repos {
		repoPath, err := p.RepoPath(r.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: %v\n", err)
			continue
		}
		wt := filepath.Join(t.Dir, r.Name)
		if err := gitx.WorktreeRemove(repoPath, wt, force); err != nil {
			fmt.Fprintf(os.Stderr, "warn: remove worktree %s: %v\n", wt, err)
		}
		if !keepBranches {
			if err := gitx.BranchDelete(repoPath, r.Branch, force); err != nil {
				fmt.Fprintf(os.Stderr, "warn: delete branch %s in %s: %v\n", r.Branch, r.Name, err)
			}
		}
		_ = gitx.WorktreePrune(repoPath)
	}
	return os.RemoveAll(t.Dir)
}

// Get loads one task by name.
func Get(p *project.Project, name string) (*Task, error) {
	dir := p.TaskDir(name)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("task %q not found", name)
	}
	m, err := readMeta(dir)
	if err != nil {
		return nil, fmt.Errorf("read task meta: %w", err)
	}
	return &Task{Meta: *m, Dir: dir}, nil
}

// List returns every task in the project, sorted by name.
func List(p *project.Project) ([]Task, error) {
	root := p.WorktreesDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Task
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		m, err := readMeta(dir)
		if err != nil {
			continue
		}
		out = append(out, Task{Meta: *m, Dir: dir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Meta.Name < out[j].Meta.Name })
	return out, nil
}

// RegenerateAgents writes AGENTS.md / CLAUDE.md from .forest/agents/*.md for one task.
func RegenerateAgents(p *project.Project, m *Meta, taskDir string) error {
	repos := make([]agents.RepoRef, len(m.Repos))
	for i, r := range m.Repos {
		repos[i] = agents.RepoRef{Name: r.Name, Branch: r.Branch, Base: r.Base}
	}
	content, err := agents.Render(p.AgentsDir(), agents.TaskMeta{
		Name:      m.Name,
		Repos:     repos,
		CreatedAt: m.CreatedAt,
	})
	if err != nil {
		return err
	}
	return agents.WriteAll(taskDir, content)
}

// RegenerateAllAgents iterates every task and regenerates its agent files.
func RegenerateAllAgents(p *project.Project) error {
	tasks, err := List(p)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if err := RegenerateAgents(p, &t.Meta, t.Dir); err != nil {
			return fmt.Errorf("task %s: %w", t.Meta.Name, err)
		}
	}
	return nil
}

func writeMeta(dir string, m Meta) error {
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, metaFile), b, 0o644)
}

func readMeta(dir string) (*Meta, error) {
	b, err := os.ReadFile(filepath.Join(dir, metaFile))
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// rollback removes any worktrees that were created before a failure mid-Create.
func rollback(p *project.Project, worktreePaths []string, branch string) {
	for _, wt := range worktreePaths {
		// Find owning repo by checking which configured repo path is parent.
		for _, r := range p.Config.Repos {
			rp, err := p.RepoPath(r.Name)
			if err != nil {
				continue
			}
			if filepath.Base(wt) == r.Name {
				_ = gitx.WorktreeRemove(rp, wt, true)
				_ = gitx.BranchDelete(rp, branch, true)
				_ = gitx.WorktreePrune(rp)
				break
			}
		}
	}
}

// SuggestRepoOrder returns repos in the order they appear in config (filtered
// to those that exist on disk).
func SuggestRepoOrder(p *project.Project) []string {
	var out []string
	for _, r := range p.Config.Repos {
		rp, err := p.RepoPath(r.Name)
		if err != nil {
			continue
		}
		if st, err := os.Stat(rp); err == nil && st.IsDir() {
			out = append(out, r.Name)
		}
	}
	return out
}

// IgnoreInExclude appends a path to <repo>/.git/info/exclude (idempotent).
func IgnoreInExclude(repoDir, line string) error {
	excl := filepath.Join(repoDir, ".git", "info", "exclude")
	b, err := os.ReadFile(excl)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, ln := range splitLines(string(b)) {
		if ln == line {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(excl), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(excl, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(b) > 0 && b[len(b)-1] != '\n' {
		f.WriteString("\n")
	}
	_, err = fmt.Fprintln(f, line)
	return err
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
