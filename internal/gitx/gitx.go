package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Run executes `git` in dir with args. Returns combined stdout (trimmed) and an
// error wrapping stderr if git exits non-zero.
func Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo(dir string) bool {
	_, err := Run(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

// BranchExists reports whether branch (local or remote-tracking) exists in repo.
func BranchExists(repoDir, branch string) bool {
	_, err := Run(repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// CurrentBranch returns the branch name HEAD points at, or "" if detached.
func CurrentBranch(dir string) (string, error) {
	out, err := Run(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", nil
	}
	return out, nil
}

// WorktreeAdd creates a worktree at path. If newBranch != "", it creates a new
// branch of that name from base. Otherwise it checks out the existing branch
// named by checkout (base is ignored). Exactly one of newBranch/checkout must
// be set. Branch/base names are expected to be validated by the caller.
func WorktreeAdd(repoDir, path, newBranch, checkout, base string) error {
	var args []string
	if newBranch != "" {
		// git worktree add -b <branch> <path> [<base>]
		args = []string{"worktree", "add", "-b", newBranch, path}
		if base != "" {
			args = append(args, base)
		}
	} else {
		// git worktree add <path> <existing-branch>
		args = []string{"worktree", "add", path, checkout}
	}
	_, err := Run(repoDir, args...)
	return err
}

// WorktreeRemove removes a worktree (force if requested).
func WorktreeRemove(repoDir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := Run(repoDir, args...)
	return err
}

// WorktreePrune cleans up stale worktree admin entries.
func WorktreePrune(repoDir string) error {
	_, err := Run(repoDir, "worktree", "prune")
	return err
}

// Worktree is one entry from `git worktree list`.
type Worktree struct {
	Path     string // absolute path; empty/"(prunable)" entries may have a reason
	Branch   string // short branch name, "" if detached
	Prunable bool   // git considers this entry removable (missing working dir)
}

// Worktrees lists the worktrees registered for the repo at repoDir.
func Worktrees(repoDir string) ([]Worktree, error) {
	out, err := Run(repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var (
		list []Worktree
		cur  Worktree
		open bool
	)
	flush := func() {
		if open {
			list = append(list, cur)
		}
		cur = Worktree{}
		open = false
	}
	for _, ln := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(ln, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(ln, "worktree ")
			open = true
		case strings.HasPrefix(ln, "branch "):
			ref := strings.TrimPrefix(ln, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case strings.HasPrefix(ln, "prunable"):
			cur.Prunable = true
		case ln == "":
			flush()
		}
	}
	flush()
	return list, nil
}

// BranchDelete deletes a local branch (force if requested).
func BranchDelete(repoDir, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := Run(repoDir, "branch", flag, branch)
	return err
}

// Status holds the cleanliness summary of a worktree.
type Status struct {
	Clean      bool
	Modified   int
	Staged     int
	Untracked  int
	Branch     string
	LastCommit string // first line of `git log -1 --format=%s`
}

// Stat returns a populated Status for dir.
func Stat(dir string) (*Status, error) {
	out, err := Run(dir, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return nil, err
	}
	s := &Status{Clean: true}
	for _, ln := range strings.Split(out, "\n") {
		if len(ln) < 2 {
			continue
		}
		s.Clean = false
		x, y := ln[0], ln[1]
		switch {
		case x == '?' && y == '?':
			s.Untracked++
		case y != ' ':
			// Work-tree column set: unstaged modification (possibly also staged).
			s.Modified++
			if x != ' ' {
				s.Staged++
			}
		default:
			// Only the index column is set: staged change.
			s.Staged++
		}
	}
	br, _ := CurrentBranch(dir)
	s.Branch = br
	if last, err := Run(dir, "log", "-1", "--format=%s"); err == nil {
		s.LastCommit = last
	}
	return s, nil
}

// AheadBehind returns commits ahead/behind of base (e.g. "main"). If base is
// missing locally, returns (0, 0, nil).
func AheadBehind(dir, base string) (ahead, behind int, err error) {
	if !BranchExists(dir, base) {
		return 0, 0, nil
	}
	out, err := Run(dir, "rev-list", "--left-right", "--count", "HEAD..."+base)
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(out)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", out)
	}
	ahead, _ = strconv.Atoi(parts[0])
	behind, _ = strconv.Atoi(parts[1])
	return ahead, behind, nil
}

// HasUnpushed reports whether the current branch (HEAD) has commits not present
// on any remote ref. Returns (false, nil) if no remotes are configured. Only
// HEAD is considered — unrelated local branches (e.g. a local main ahead of
// origin) do not count.
func HasUnpushed(dir string) (bool, error) {
	remotes, err := Run(dir, "remote")
	if err != nil || remotes == "" {
		return false, nil
	}
	// Commits reachable from HEAD but from no remote-tracking ref.
	out, err := Run(dir, "log", "HEAD", "--not", "--remotes", "--oneline", "-n", "1")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// DefaultBranch reports the repository's default branch name. It tries the
// remote HEAD symbolic ref (origin/HEAD), then falls back to the currently
// checked-out branch, then "main".
func DefaultBranch(dir string) string {
	if out, err := Run(dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		// e.g. "origin/main" -> "main"
		if i := strings.IndexByte(out, '/'); i >= 0 && i+1 < len(out) {
			return out[i+1:]
		}
	}
	if br, _ := CurrentBranch(dir); br != "" {
		return br
	}
	return "main"
}
