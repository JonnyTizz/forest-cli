package runner

import (
	"reflect"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		err  bool
	}{
		{"vim", []string{"vim"}, false},
		{"code --wait", []string{"code", "--wait"}, false},
		{"  spaced   out ", []string{"spaced", "out"}, false},
		{`"my editor" -n`, []string{"my editor", "-n"}, false},
		{`emacs '--option with space'`, []string{"emacs", "--option with space"}, false},
		{`a\ b`, []string{"a b"}, false},
		{"", nil, false},
		{`"unterminated`, nil, true},
		{`trailing\`, nil, true},
	}
	for _, c := range cases {
		got, err := SplitArgs(c.in)
		if c.err {
			if err == nil {
				t.Errorf("SplitArgs(%q): expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("SplitArgs(%q): %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitArgs(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
