package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

// initRepo creates a git repo at dir with one commit on the given default
// branch and returns dir.
func initRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q", "-b", branch)
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := Run(dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func TestIsRepoAndCurrentBranch(t *testing.T) {
	dir := initRepo(t, "main")
	if !IsRepo(dir) {
		t.Error("IsRepo = false for a git repo")
	}
	if IsRepo(t.TempDir()) {
		t.Error("IsRepo = true for a non-repo")
	}
	br, err := CurrentBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if br != "main" {
		t.Errorf("CurrentBranch = %q, want main", br)
	}
}

func TestBranchExists(t *testing.T) {
	dir := initRepo(t, "main")
	if !BranchExists(dir, "main") {
		t.Error("main should exist")
	}
	if BranchExists(dir, "nope") {
		t.Error("nope should not exist")
	}
}

func TestStat(t *testing.T) {
	dir := initRepo(t, "main")
	s, err := Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Clean {
		t.Errorf("fresh repo not clean: %+v", s)
	}

	// Untracked file.
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644)
	s, _ = Stat(dir)
	if s.Clean || s.Untracked != 1 {
		t.Errorf("untracked: %+v", s)
	}

	// Staged new file.
	mustGit(t, dir, "add", "new.txt")
	s, _ = Stat(dir)
	if s.Staged != 1 {
		t.Errorf("staged: %+v", s)
	}

	// Modified tracked file (unstaged).
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	s, _ = Stat(dir)
	if s.Modified < 1 {
		t.Errorf("modified: %+v", s)
	}
	if s.LastCommit != "initial" {
		t.Errorf("LastCommit = %q", s.LastCommit)
	}
}

func TestDefaultBranch(t *testing.T) {
	if got := DefaultBranch(initRepo(t, "main")); got != "main" {
		t.Errorf("DefaultBranch = %q, want main", got)
	}
	if got := DefaultBranch(initRepo(t, "master")); got != "master" {
		t.Errorf("DefaultBranch = %q, want master", got)
	}
}

func TestWorktreeAddNewAndExistingBranch(t *testing.T) {
	repo := initRepo(t, "main")
	parent := t.TempDir()

	// New branch from main.
	wt1 := filepath.Join(parent, "wt1")
	if err := WorktreeAdd(repo, wt1, "feature", "", "main"); err != nil {
		t.Fatalf("new-branch worktree: %v", err)
	}
	if br, _ := CurrentBranch(wt1); br != "feature" {
		t.Fatalf("wt1 branch = %q, want feature", br)
	}

	// Create another branch directly, then check it out into a second worktree
	// via the existing-branch path. This is the A1 regression: it must check out
	// the branch, not the base.
	mustGit(t, repo, "branch", "existing", "main")
	wt2 := filepath.Join(parent, "wt2")
	if err := WorktreeAdd(repo, wt2, "", "existing", "main"); err != nil {
		t.Fatalf("existing-branch worktree: %v", err)
	}
	if br, _ := CurrentBranch(wt2); br != "existing" {
		t.Fatalf("wt2 branch = %q, want existing (A1 regression)", br)
	}
}

func TestHasUnpushedScopedToHEAD(t *testing.T) {
	// Bare "remote".
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")

	local := initRepo(t, "main")
	mustGit(t, local, "remote", "add", "origin", remote)
	mustGit(t, local, "push", "-q", "origin", "main")

	// HEAD (main) is fully pushed -> no unpushed.
	if has, _ := HasUnpushed(local); has {
		t.Error("main is pushed; HasUnpushed should be false")
	}

	// Create a feature branch with a commit, but stay on main. Because the check
	// is scoped to HEAD, main should still report no unpushed commits.
	mustGit(t, local, "checkout", "-q", "-b", "feature")
	os.WriteFile(filepath.Join(local, "f.txt"), []byte("x"), 0o644)
	mustGit(t, local, "add", ".")
	mustGit(t, local, "commit", "-q", "-m", "feature work")
	mustGit(t, local, "checkout", "-q", "main")
	if has, _ := HasUnpushed(local); has {
		t.Error("HEAD=main is pushed; an unpushed feature branch must not count (A5)")
	}

	// On the feature branch, HEAD has unpushed commits.
	mustGit(t, local, "checkout", "-q", "feature")
	if has, _ := HasUnpushed(local); !has {
		t.Error("feature branch has unpushed commits; HasUnpushed should be true")
	}
}

func TestWorktrees(t *testing.T) {
	repo := initRepo(t, "main")
	wt := filepath.Join(t.TempDir(), "wt")
	if err := WorktreeAdd(repo, wt, "feature", "", "main"); err != nil {
		t.Fatal(err)
	}
	list, err := Worktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range list {
		if w.Branch == "feature" {
			found = true
		}
	}
	if !found {
		t.Errorf("feature worktree not listed: %+v", list)
	}
}
