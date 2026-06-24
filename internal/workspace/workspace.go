package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

// ValidateRef rejects branch/base names that git could mistake for an option
// or that are otherwise unsafe to pass on a git command line. It is deliberately
// conservative rather than a full check of git's ref rules.
func ValidateRef(ref string) error {
	switch {
	case ref == "":
		return errors.New("empty branch/base name")
	case strings.HasPrefix(ref, "-"):
		return fmt.Errorf("invalid ref %q (must not start with '-')", ref)
	case strings.ContainsAny(ref, " \t\n:?*[\\^~"):
		return fmt.Errorf("invalid ref %q (contains whitespace or a git-special character)", ref)
	case strings.Contains(ref, ".."):
		return fmt.Errorf("invalid ref %q (contains '..')", ref)
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
	if err := ValidateRef(branch); err != nil {
		return nil, err
	}
	if opts.Base != "" {
		if err := ValidateRef(opts.Base); err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return nil, err
	}

	meta := Meta{Name: opts.Name, CreatedAt: time.Now()}
	var created []createdWorktree // for rollback on any later failure

	// fail rolls back everything created so far and removes the task dir.
	fail := func(err error) (*Task, error) {
		rollback(created)
		os.RemoveAll(taskDir)
		return nil, err
	}

	for _, repoName := range opts.Repos {
		repoCfg, ok := p.Config.FindRepo(repoName)
		if !ok {
			return fail(fmt.Errorf("repo %q not configured", repoName))
		}
		repoPath, err := p.RepoPath(repoName)
		if err != nil {
			return fail(err)
		}
		base := opts.Base
		if base == "" {
			base = repoCfg.DefaultBase
		}
		wtPath := filepath.Join(taskDir, repoName)
		branchExisted := gitx.BranchExists(repoPath, branch)
		newBranch, checkout := branch, ""
		if branchExisted {
			newBranch, checkout = "", branch // check out the existing branch
		}
		if err := gitx.WorktreeAdd(repoPath, wtPath, newBranch, checkout, base); err != nil {
			return fail(fmt.Errorf("create worktree for %s: %w", repoName, err))
		}
		created = append(created, createdWorktree{
			repoPath:     repoPath,
			path:         wtPath,
			branch:       branch,
			branchByThis: !branchExisted,
		})
		meta.Repos = append(meta.Repos, RepoMeta{Name: repoName, Branch: branch, Base: base})
	}

	if err := writeMeta(taskDir, meta); err != nil {
		return fail(fmt.Errorf("write task meta: %w", err))
	}
	if err := RegenerateAgents(p, &meta, taskDir); err != nil {
		return fail(fmt.Errorf("write agents files: %w", err))
	}
	if _, err := envfiles.Sync(p.Root, taskDir, p.Config.EnvFiles, false); err != nil {
		return fail(fmt.Errorf("sync env files: %w", err))
	}
	return &Task{Meta: meta, Dir: taskDir}, nil
}

// Remove tears down a task workspace. It returns any non-fatal warnings
// (failed branch deletes, etc.) so callers can surface them however they like
// instead of writing to stderr directly.
//
// Without force, a worktree that git refuses to remove (e.g. because it is
// dirty) aborts the teardown before the task directory is deleted, so the task
// is never left half-removed with stale git worktree metadata.
func Remove(p *project.Project, name string, force, keepBranches bool) (warnings []string, err error) {
	t, err := Get(p, name)
	if err != nil {
		return nil, err
	}

	// Safety pass: refuse if any worktree dirty / has unpushed.
	if !force {
		for _, r := range t.Meta.Repos {
			wt := filepath.Join(t.Dir, r.Name)
			if err := safety.EnsureClean(wt); err != nil {
				return nil, err
			}
			if err := safety.EnsureNoUnpushed(wt); err != nil {
				return nil, err
			}
		}
	}

	var removeFailed bool
	for _, r := range t.Meta.Repos {
		repoPath, err := p.RepoPath(r.Name)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%v", err))
			removeFailed = true
			continue
		}
		wt := filepath.Join(t.Dir, r.Name)
		if err := gitx.WorktreeRemove(repoPath, wt, force); err != nil {
			warnings = append(warnings, fmt.Sprintf("remove worktree %s: %v", wt, err))
			removeFailed = true
		}
		if !keepBranches {
			if err := gitx.BranchDelete(repoPath, r.Branch, force); err != nil {
				warnings = append(warnings, fmt.Sprintf("delete branch %s in %s: %v", r.Branch, r.Name, err))
			}
		}
		_ = gitx.WorktreePrune(repoPath)
	}

	// If a worktree could not be removed and we are not forcing, leave the task
	// directory in place rather than orphaning git's worktree admin entries.
	if removeFailed && !force {
		return warnings, fmt.Errorf("task %q not fully removed; rerun with --force to override", name)
	}
	if err := os.RemoveAll(t.Dir); err != nil {
		return warnings, err
	}
	// Prune now that the worktree directories are gone, clearing any admin
	// entries git still held.
	for _, r := range t.Meta.Repos {
		if repoPath, err := p.RepoPath(r.Name); err == nil {
			_ = gitx.WorktreePrune(repoPath)
		}
	}
	return warnings, nil
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

// Orphans returns absolute paths of directories under the worktrees dir that do
// not contain a readable task meta file. These are typically left behind by a
// task creation that failed partway through.
func Orphans(p *project.Project) ([]string, error) {
	root := p.WorktreesDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := readMeta(dir); err != nil {
			out = append(out, dir)
		}
	}
	sort.Strings(out)
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

// createdWorktree records a worktree made during Create, for rollback.
type createdWorktree struct {
	repoPath     string // absolute path to the owning repo
	path         string // absolute path to the worktree
	branch       string // task branch name
	branchByThis bool   // true if Create created the branch (safe to delete)
}

// rollback removes worktrees created before a failure mid-Create. It only
// deletes branches that Create itself created — a pre-existing branch that was
// merely checked out is left untouched.
func rollback(created []createdWorktree) {
	for _, c := range created {
		_ = gitx.WorktreeRemove(c.repoPath, c.path, true)
		if c.branchByThis {
			_ = gitx.BranchDelete(c.repoPath, c.branch, true)
		}
		_ = gitx.WorktreePrune(c.repoPath)
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
