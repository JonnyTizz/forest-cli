package envfiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JonnyTizz/forest/internal/config"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncActions(t *testing.T) {
	root := t.TempDir()
	task := t.TempDir()
	writeFile(t, filepath.Join(root, ".env"), "A=1\n")
	files := []config.EnvFile{{Source: ".env", Dest: ".env"}}

	// First sync: copied.
	res, err := Sync(root, task, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Action != "copied" {
		t.Fatalf("want copied, got %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(task, ".env")); string(b) != "A=1\n" {
		t.Fatalf("dest content = %q", b)
	}

	// Second sync, unchanged: skipped-identical.
	res, _ = Sync(root, task, files, false)
	if res[0].Action != "skipped-identical" {
		t.Fatalf("want skipped-identical, got %q", res[0].Action)
	}

	// Modify dest, sync without force: refused-modified.
	writeFile(t, filepath.Join(task, ".env"), "A=2\n")
	res, _ = Sync(root, task, files, false)
	if res[0].Action != "refused-modified" {
		t.Fatalf("want refused-modified, got %q", res[0].Action)
	}

	// With force: copied (overwrites).
	res, _ = Sync(root, task, files, true)
	if res[0].Action != "copied" {
		t.Fatalf("want copied with force, got %q", res[0].Action)
	}
	if b, _ := os.ReadFile(filepath.Join(task, ".env")); string(b) != "A=1\n" {
		t.Fatalf("force did not overwrite, dest = %q", b)
	}
}

func TestSyncMissingSource(t *testing.T) {
	root := t.TempDir()
	task := t.TempDir()
	files := []config.EnvFile{{Source: "nope.env", Dest: ".env"}}
	res, err := Sync(root, task, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Action != "skipped-missing-source" {
		t.Fatalf("want skipped-missing-source, got %q", res[0].Action)
	}
}

func TestSyncRejectsEscapingPaths(t *testing.T) {
	root := t.TempDir()
	task := t.TempDir()
	cases := []config.EnvFile{
		{Source: "/etc/passwd", Dest: ".env"},
		{Source: "../../secret", Dest: ".env"},
		{Source: ".env", Dest: "/tmp/exfil"},
		{Source: ".env", Dest: "../escape"},
	}
	for _, c := range cases {
		res, err := Sync(root, task, []config.EnvFile{c}, false)
		if err != nil {
			t.Fatalf("Sync returned hard error for %+v: %v", c, err)
		}
		if res[0].Err == nil {
			t.Fatalf("expected error result for %+v, got %+v", c, res[0])
		}
	}
}

func TestDiffStates(t *testing.T) {
	root := t.TempDir()
	task := t.TempDir()
	writeFile(t, filepath.Join(root, "a.env"), "X=1\n")
	writeFile(t, filepath.Join(task, "a.env"), "X=1\n")
	writeFile(t, filepath.Join(root, "b.env"), "Y=1\n")
	writeFile(t, filepath.Join(task, "b.env"), "Y=2\n")
	writeFile(t, filepath.Join(root, "c.env"), "Z=1\n") // dest missing

	files := []config.EnvFile{
		{Source: "a.env", Dest: "a.env"},
		{Source: "b.env", Dest: "b.env"},
		{Source: "c.env", Dest: "c.env"},
		{Source: "d.env", Dest: "d.env"}, // source missing
	}
	entries, err := Diff(root, task, files)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a.env": "match", "b.env": "drift", "c.env": "missing-dest", "d.env": "missing-source"}
	for _, e := range entries {
		base := filepath.Base(e.Source)
		if want[base] != e.State {
			t.Errorf("%s: state = %q, want %q", base, e.State, want[base])
		}
	}
	if !HasDrift(entries) {
		t.Error("HasDrift should be true")
	}
}
