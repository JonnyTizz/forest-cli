package envfiles

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JonnyTizz/forest/internal/config"
)

// Result captures what happened to one env file during sync.
type Result struct {
	Source string
	Dest   string
	Action string // "copied", "skipped-identical", "skipped-missing-source", "refused-modified"
	Err    error
}

// Sync copies every configured env file from projectRoot into taskDir.
// If the destination exists and differs from the source, the file is left
// untouched and the result reports "refused-modified" — unless force is true.
func Sync(projectRoot, taskDir string, files []config.EnvFile, force bool) ([]Result, error) {
	results := make([]Result, 0, len(files))
	for _, f := range files {
		src := absUnder(projectRoot, f.Source)
		dst := absUnder(taskDir, f.Dest)
		r := Result{Source: src, Dest: dst}
		srcBytes, err := os.ReadFile(src)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				r.Action = "skipped-missing-source"
				results = append(results, r)
				continue
			}
			r.Err = err
			results = append(results, r)
			continue
		}
		if existing, err := os.ReadFile(dst); err == nil {
			if bytes.Equal(existing, srcBytes) {
				r.Action = "skipped-identical"
				results = append(results, r)
				continue
			}
			if !force {
				r.Action = "refused-modified"
				results = append(results, r)
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			r.Err = err
			results = append(results, r)
			continue
		}
		if err := os.WriteFile(dst, srcBytes, 0o644); err != nil {
			r.Err = err
			results = append(results, r)
			continue
		}
		r.Action = "copied"
		results = append(results, r)
	}
	return results, nil
}

// DriftEntry describes a per-file result of a diff scan.
type DriftEntry struct {
	Source string
	Dest   string
	State  string // "match", "drift", "missing-source", "missing-dest"
	Diff   string // unified diff when State == "drift"
}

// Diff inspects each configured env file and returns the drift summary.
func Diff(projectRoot, taskDir string, files []config.EnvFile) ([]DriftEntry, error) {
	out := make([]DriftEntry, 0, len(files))
	for _, f := range files {
		src := absUnder(projectRoot, f.Source)
		dst := absUnder(taskDir, f.Dest)
		e := DriftEntry{Source: src, Dest: dst}
		sb, sErr := os.ReadFile(src)
		db, dErr := os.ReadFile(dst)
		switch {
		case errors.Is(sErr, os.ErrNotExist):
			e.State = "missing-source"
		case errors.Is(dErr, os.ErrNotExist):
			e.State = "missing-dest"
		case sErr != nil:
			return nil, sErr
		case dErr != nil:
			return nil, dErr
		case bytes.Equal(sb, db):
			e.State = "match"
		default:
			e.State = "drift"
			e.Diff = unifiedLineDiff(string(sb), string(db), f.Source, f.Dest)
		}
		out = append(out, e)
	}
	return out, nil
}

// HasDrift returns true if any entry is in "drift" state.
func HasDrift(entries []DriftEntry) bool {
	for _, e := range entries {
		if e.State == "drift" {
			return true
		}
	}
	return false
}

func absUnder(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

// unifiedLineDiff is a minimal, line-oriented diff that flags every changed
// line. Good enough for env files; not a full LCS implementation.
func unifiedLineDiff(a, b, aLabel, bLabel string) string {
	aLines := splitKeepNL(a)
	bLines := splitKeepNL(b)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "--- %s\n+++ %s\n", aLabel, bLabel)
	n := max(len(aLines), len(bLines))
	for i := 0; i < n; i++ {
		var al, bl string
		if i < len(aLines) {
			al = aLines[i]
		}
		if i < len(bLines) {
			bl = bLines[i]
		}
		if al == bl {
			continue
		}
		if al != "" {
			fmt.Fprintf(&buf, "-%s", al)
			if !strings.HasSuffix(al, "\n") {
				buf.WriteByte('\n')
			}
		}
		if bl != "" {
			fmt.Fprintf(&buf, "+%s", bl)
			if !strings.HasSuffix(bl, "\n") {
				buf.WriteByte('\n')
			}
		}
	}
	return buf.String()
}

func splitKeepNL(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			return out
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
		if s == "" {
			return out
		}
	}
}
