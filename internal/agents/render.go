package agents

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RepoRef is a minimal repo summary used in the rendered header.
type RepoRef struct {
	Name   string
	Branch string
	Base   string
}

// TaskMeta supplies header info for the rendered AGENTS.md.
type TaskMeta struct {
	Name      string
	Repos     []RepoRef
	CreatedAt time.Time
}

// Render concatenates every *.md file in agentsDir (sorted by filename) under a
// task header derived from meta. The result is suitable to write to AGENTS.md
// and CLAUDE.md.
func Render(agentsDir string, meta TaskMeta) ([]byte, error) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	var buf bytes.Buffer
	writeHeader(&buf, meta)
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(agentsDir, f))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, "\n<!-- agents/%s -->\n\n", f)
		buf.Write(b)
		if !bytes.HasSuffix(b, []byte("\n")) {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

// WriteAll writes AGENTS.md and a copy CLAUDE.md into taskDir.
func WriteAll(taskDir string, content []byte) error {
	if err := os.WriteFile(filepath.Join(taskDir, "AGENTS.md"), content, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(taskDir, "CLAUDE.md"), content, 0o644)
}

func writeHeader(buf *bytes.Buffer, m TaskMeta) {
	fmt.Fprintf(buf, "# Task: %s\n\n", m.Name)
	if !m.CreatedAt.IsZero() {
		fmt.Fprintf(buf, "_Created %s_\n\n", m.CreatedAt.UTC().Format(time.RFC3339))
	}
	if len(m.Repos) > 0 {
		buf.WriteString("## Repos in this workspace\n\n")
		buf.WriteString("| Repo | Branch | Base |\n")
		buf.WriteString("|------|--------|------|\n")
		for _, r := range m.Repos {
			fmt.Fprintf(buf, "| %s | %s | %s |\n", r.Name, r.Branch, r.Base)
		}
		buf.WriteString("\n")
	}
	buf.WriteString("---\n")
}
