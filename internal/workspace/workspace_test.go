package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JonnyTizz/forest/internal/config"
	"github.com/JonnyTizz/forest/internal/gitx"
	"github.com/JonnyTizz/forest/internal/project"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := gitx.Run(dir, args...); err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
}

// newRepo makes a git repo with one commit on main.
func newRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "t@e.com")
	git(t, dir, "config", "user.name", "T")
	os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "init")
}

// newProject builds a project root with the named repos (each a git repo) and a
// valid config, then loads it.
func newProject(t *testing.T, repos ...string) *project.Project {
	t.Helper()
	root := t.TempDir()
	cfg := config.Defaults()
	for _, r := range repos {
		newRepo(t, filepath.Join(root, r))
		cfg.Repos = append(cfg.Repos, config.Repo{Name: r, Path: r, DefaultBase: "main"})
	}
	forest := filepath.Join(root, config.Dirname)
	if err := os.MkdirAll(filepath.Join(forest, config.AgentsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(forest, config.AgentsDir, "00.md"), []byte("ctx\n"), 0o644)
	if err := config.Save(filepath.Join(forest, config.ConfigFile), &cfg); err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreateAndRemove(t *testing.T) {
	p := newProject(t, "repo1", "repo2")
	task, err := Create(p, CreateOptions{Name: "feat", Repos: []string{"repo1", "repo2"}})
	if err != nil {
		t.Fatal(err)
	}
	// Worktrees and generated files exist.
	for _, r := range []string{"repo1", "repo2"} {
		wt := filepath.Join(task.Dir, r)
		if br, _ := gitx.CurrentBranch(wt); br != "task/feat" {
			t.Errorf("%s branch = %q, want task/feat", r, br)
		}
	}
	for _, f := range []string{"AGENTS.md", "CLAUDE.md", metaFile} {
		if _, err := os.Stat(filepath.Join(task.Dir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}

	// Duplicate create fails.
	if _, err := Create(p, CreateOptions{Name: "feat", Repos: []string{"repo1"}}); err == nil {
		t.Error("duplicate create should fail")
	}

	// Remove (clean) succeeds and deletes branches + dir.
	warns, err := Remove(p, "feat", false, false)
	if err != nil {
		t.Fatalf("remove: %v (warnings %v)", err, warns)
	}
	if _, err := os.Stat(task.Dir); !os.IsNotExist(err) {
		t.Error("task dir should be gone")
	}
	if gitx.BranchExists(filepath.Join(p.Root, "repo1"), "task/feat") {
		t.Error("task/feat branch should be deleted")
	}
}

func TestRemoveRefusesDirty(t *testing.T) {
	p := newProject(t, "repo1")
	task, err := Create(p, CreateOptions{Name: "dirty", Repos: []string{"repo1"}})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(task.Dir, "repo1", "scratch"), []byte("wip"), 0o644)

	if _, err := Remove(p, "dirty", false, false); err == nil {
		t.Error("remove should refuse a dirty worktree")
	}
	if _, err := os.Stat(task.Dir); err != nil {
		t.Error("task dir should still exist after refused remove")
	}

	// Force removes it.
	if _, err := Remove(p, "dirty", true, false); err != nil {
		t.Fatalf("force remove: %v", err)
	}
	if _, err := os.Stat(task.Dir); !os.IsNotExist(err) {
		t.Error("task dir should be gone after force remove")
	}
}

func TestCreateExistingBranchIsCheckedOut(t *testing.T) {
	p := newProject(t, "repo1")
	repo := filepath.Join(p.Root, "repo1")
	git(t, repo, "branch", "task/reuse", "main")

	task, err := Create(p, CreateOptions{Name: "reuse", Repos: []string{"repo1"}})
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(task.Dir, "repo1")
	if br, _ := gitx.CurrentBranch(wt); br != "task/reuse" {
		t.Errorf("worktree branch = %q, want task/reuse (A1)", br)
	}
}

func TestRollbackPreservesExistingBranch(t *testing.T) {
	// repo1 is valid and has a pre-existing task branch; repo2 is configured but
	// is NOT a git repo, so worktree creation for it fails after repo1 succeeds.
	root := t.TempDir()
	newRepo(t, filepath.Join(root, "repo1"))
	os.MkdirAll(filepath.Join(root, "repo2"), 0o755) // not a git repo

	cfg := config.Defaults()
	cfg.Repos = []config.Repo{
		{Name: "repo1", Path: "repo1", DefaultBase: "main"},
		{Name: "repo2", Path: "repo2", DefaultBase: "main"},
	}
	forest := filepath.Join(root, config.Dirname)
	os.MkdirAll(filepath.Join(forest, config.AgentsDir), 0o755)
	config.Save(filepath.Join(forest, config.ConfigFile), &cfg)
	p, err := project.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	repo1 := filepath.Join(root, "repo1")
	git(t, repo1, "branch", "task/x", "main")

	_, err = Create(p, CreateOptions{Name: "x", Repos: []string{"repo1", "repo2"}})
	if err == nil {
		t.Fatal("expected create to fail on repo2")
	}
	// Task dir must be cleaned up.
	if _, err := os.Stat(p.TaskDir("x")); !os.IsNotExist(err) {
		t.Error("task dir should be rolled back")
	}
	// A3: the pre-existing branch must NOT have been deleted by rollback.
	if !gitx.BranchExists(repo1, "task/x") {
		t.Error("rollback deleted a pre-existing branch it did not create (A3)")
	}
}

func TestOrphans(t *testing.T) {
	p := newProject(t, "repo1")
	if _, err := Create(p, CreateOptions{Name: "real", Repos: []string{"repo1"}}); err != nil {
		t.Fatal(err)
	}
	// Make an orphan dir (no meta) directly under the worktrees root.
	orphan := filepath.Join(p.WorktreesDir(), "ghost")
	os.MkdirAll(orphan, 0o755)

	orphans, err := Orphans(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || filepath.Base(orphans[0]) != "ghost" {
		t.Errorf("orphans = %v, want [ghost]", orphans)
	}
}

func TestValidateName(t *testing.T) {
	good := []string{"a", "feat-1", "a.b_c", "x123"}
	bad := []string{"", "UPPER", "has space", "/slash", "-leading", "with/slash"}
	for _, n := range good {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}
	for _, n := range bad {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", n)
		}
	}
}

func TestValidateRef(t *testing.T) {
	good := []string{"main", "task/foo", "release-1.2"}
	bad := []string{"", "-x", "a b", "a..b", "a:b", "a~b"}
	for _, r := range good {
		if err := ValidateRef(r); err != nil {
			t.Errorf("ValidateRef(%q) = %v, want nil", r, err)
		}
	}
	for _, r := range bad {
		if err := ValidateRef(r); err == nil {
			t.Errorf("ValidateRef(%q) = nil, want error", r)
		}
	}
}
