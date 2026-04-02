package config

import "testing"

func TestMatchRule_ToolOnly(t *testing.T) {
	cases := []struct {
		rule, tool, arg string
		want            bool
	}{
		{"Bash", "Bash", "git status", true},
		{"Bash", "bash", "anything", true}, // case-insensitive
		{"Read", "Bash", "git status", false},
		{"Bash(git *)", "Bash", "git status", true},
		{"Bash(git *)", "Bash", "npm install", false},
		{"Bash(npm run:*)", "Bash", "npm run test", true},
		{"Bash(npm run:*)", "Bash", "npm install", false},
		{"Read", "Read", "/path/to/file", true},
		{"Read(/path/**)", "Read", "/path/to/file", false}, // filepath.Match doesn't handle **
	}
	for _, c := range cases {
		got := MatchRule(c.rule, c.tool, c.arg)
		if got != c.want {
			t.Errorf("MatchRule(%q, %q, %q) = %v, want %v", c.rule, c.tool, c.arg, got, c.want)
		}
	}
}
