package config

import "testing"

func base() *ProjectConfig {
	c := Defaults()
	c.Repos = []Repo{{Name: "r", Path: "r", DefaultBase: "main"}}
	return &c
}

func TestValidateOK(t *testing.T) {
	if err := base().Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidateDefaultsBase(t *testing.T) {
	c := base()
	c.Repos[0].DefaultBase = ""
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Repos[0].DefaultBase != "main" {
		t.Errorf("DefaultBase not defaulted: %q", c.Repos[0].DefaultBase)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := map[string]func(*ProjectConfig){
		"bad version":   func(c *ProjectConfig) { c.Version = 2 },
		"no repos":      func(c *ProjectConfig) { c.Repos = nil },
		"empty name":    func(c *ProjectConfig) { c.Repos[0].Name = "" },
		"slash name":    func(c *ProjectConfig) { c.Repos[0].Name = "a/b" },
		"empty path":    func(c *ProjectConfig) { c.Repos[0].Path = "" },
		"dup name":      func(c *ProjectConfig) { c.Repos = append(c.Repos, c.Repos[0]) },
		"bad agent dft": func(c *ProjectConfig) { c.Agents.Default = "missing" },
	}
	for name, mut := range cases {
		c := base()
		mut(c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}
