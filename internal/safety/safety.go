package safety

import (
	"fmt"
	"strings"

	"github.com/JonnyTizz/forest/internal/gitx"
)

// DirtyError is returned when a worktree has uncommitted/untracked changes.
type DirtyError struct {
	Path   string
	Status *gitx.Status
}

func (e *DirtyError) Error() string {
	var parts []string
	if e.Status.Modified > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", e.Status.Modified))
	}
	if e.Status.Staged > 0 {
		parts = append(parts, fmt.Sprintf("%d staged", e.Status.Staged))
	}
	if e.Status.Untracked > 0 {
		parts = append(parts, fmt.Sprintf("%d untracked", e.Status.Untracked))
	}
	return fmt.Sprintf("worktree %s is dirty (%s)", e.Path, strings.Join(parts, ", "))
}

// UnpushedError is returned when a worktree has commits not on any remote.
type UnpushedError struct {
	Path string
}

func (e *UnpushedError) Error() string {
	return fmt.Sprintf("worktree %s has unpushed commits", e.Path)
}

// EnsureClean returns a *DirtyError if the worktree at path has any
// modifications, staged changes, or untracked files.
func EnsureClean(path string) error {
	s, err := gitx.Stat(path)
	if err != nil {
		return err
	}
	if !s.Clean {
		return &DirtyError{Path: path, Status: s}
	}
	return nil
}

// EnsureNoUnpushed returns an *UnpushedError if HEAD has commits absent from
// every remote. No-op when no remotes are configured.
func EnsureNoUnpushed(path string) error {
	has, err := gitx.HasUnpushed(path)
	if err != nil {
		return err
	}
	if has {
		return &UnpushedError{Path: path}
	}
	return nil
}
