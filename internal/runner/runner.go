package runner

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/JonnyTizz/forest/internal/config"
)

// Resolve looks up a named entry in a CommandSet, falling back to the default.
// Returns the spec plus the effective name used.
func Resolve(set config.CommandSet, name string) (config.CommandSpec, string, error) {
	if name == "" {
		name = set.Default
	}
	if name == "" {
		return config.CommandSpec{}, "", fmt.Errorf("no command name supplied and no default configured")
	}
	spec, ok := set.Options[name]
	if !ok {
		return config.CommandSpec{}, "", fmt.Errorf("unknown command %q (known: %v)", name, keys(set.Options))
	}
	return spec, name, nil
}

// ExecReplace runs spec from cwd, replacing the current process. Used for
// agents so signals/TTY pass through cleanly.
func ExecReplace(cwd string, spec config.CommandSpec, extraArgs []string) error {
	bin, err := exec.LookPath(spec.Command)
	if err != nil {
		return fmt.Errorf("lookup %s: %w", spec.Command, err)
	}
	if err := os.Chdir(cwd); err != nil {
		return err
	}
	argv := append([]string{bin}, append(spec.Args, extraArgs...)...)
	return syscall.Exec(bin, argv, os.Environ())
}

// Spawn launches spec from cwd as a child process and waits for it.
// For editors that fork+detach (e.g. cursor, code) this returns quickly.
func Spawn(cwd string, spec config.CommandSpec, extraArgs []string) error {
	args := append(append([]string{}, spec.Args...), extraArgs...)
	cmd := exec.Command(spec.Command, args...)
	cmd.Dir = cwd
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// EditorCommand resolves the user's preferred terminal editor: $EDITOR, then
// $VISUAL, then "vi". Used by `forest config edit`.
func EditorCommand() string {
	for _, k := range []string{"EDITOR", "VISUAL"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "vi"
}

// SplitArgs splits a command string into argv-style words, honouring single and
// double quotes and backslash escapes. It is a minimal shell-style splitter —
// enough to parse values like `code --wait` or `"my editor" -n` — and it does
// not perform any expansion, so the result is safe to pass to exec.Command
// without a shell.
func SplitArgs(s string) ([]string, error) {
	var (
		args    []string
		cur     []rune
		inWord  bool
		quote   rune // 0, '\'' or '"'
		escaped bool
	)
	flush := func() {
		if inWord {
			args = append(args, string(cur))
			cur = cur[:0]
			inWord = false
		}
	}
	for _, r := range s {
		switch {
		case escaped:
			cur = append(cur, r)
			inWord = true
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur = append(cur, r)
			}
		case quote == '"':
			if r == '"' {
				quote = 0
			} else if r == '\\' {
				escaped = true
			} else {
				cur = append(cur, r)
			}
		case r == '\\':
			escaped = true
			inWord = true
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			cur = append(cur, r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if escaped {
		return nil, fmt.Errorf("trailing backslash")
	}
	flush()
	return args, nil
}

func keys(m map[string]config.CommandSpec) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
