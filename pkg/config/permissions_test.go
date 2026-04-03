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
		{"Read(/path/**)", "Read", "/path/to/file", true}, // doublestar handles **
	}
	for _, c := range cases {
		got := MatchRule(c.rule, c.tool, c.arg)
		if got != c.want {
			t.Errorf("MatchRule(%q, %q, %q) = %v, want %v", c.rule, c.tool, c.arg, got, c.want)
		}
	}
}

func TestParsePermissionRules(t *testing.T) {
	tests := []struct {
		name     string
		input    PermissionsSettings
		expected PermissionRules
	}{
		{
			name:     "empty settings",
			input:    PermissionsSettings{},
			expected: PermissionRules{},
		},
		{
			name: "with allow rules",
			input: PermissionsSettings{
				Allow: []string{"Bash(git *)", "Read(*)"},
			},
			expected: PermissionRules{
				Allow: []string{"Bash(git *)", "Read(*)"},
			},
		},
		{
			name: "with all rule types",
			input: PermissionsSettings{
				Allow: []string{"Bash(go *)"},
				Deny:  []string{"Bash(rm *)"},
				Ask:   []string{"Bash(curl *)"},
			},
			expected: PermissionRules{
				Allow: []string{"Bash(go *)"},
				Deny:  []string{"Bash(rm *)"},
				Ask:   []string{"Bash(curl *)"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePermissionRules(tt.input)
			if len(result.Allow) != len(tt.expected.Allow) {
				t.Errorf("Allow: got %v, want %v", result.Allow, tt.expected.Allow)
			}
			if len(result.Deny) != len(tt.expected.Deny) {
				t.Errorf("Deny: got %v, want %v", result.Deny, tt.expected.Deny)
			}
			if len(result.Ask) != len(tt.expected.Ask) {
				t.Errorf("Ask: got %v, want %v", result.Ask, tt.expected.Ask)
			}
		})
	}
}

func TestMatchRule_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		rule     string
		toolName string
		arg      string
		expected bool
	}{
		// Exact tool name match
		{"exact match", "Bash", "Bash", "any command", true},
		{"case insensitive match", "BASH", "Bash", "any command", true},
		{"tool name mismatch", "Bash", "Read", "any path", false},

		// Pattern matching with parens
		{"pattern match - git command", "Bash(git *)", "Bash", "git status", true},
		{"pattern match - not matching", "Bash(git *)", "Bash", "npm install", false},
		{"pattern match - glob", "Read(src/*.go)", "Read", "src/main.go", true},

		// Prefix syntax (run:*)
		{"prefix match - npm run", "Bash(npm run:*)", "Bash", "npm run test", true},
		{"prefix match - exact prefix", "Bash(npm run:*)", "Bash", "npm run build", true},
		{"prefix match - not matching", "Bash(npm run:*)", "Bash", "npm install", false},

		// Invalid patterns
		{"unclosed paren", "Bash(git *", "Bash", "git status", false},
		{"no close paren", "Read(", "Read", "file.txt", false},

		// Bash special glob matching (allows / in *)
		{"bash glob with path", "Bash(git *)", "Bash", "git -C /path/to/repo status", true},

		// Empty arg
		{"empty arg with pattern", "Bash(git *)", "Bash", "", false},
		{"empty arg with tool only", "Bash", "Bash", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchRule(tt.rule, tt.toolName, tt.arg)
			if result != tt.expected {
				t.Errorf("MatchRule(%q, %q, %q) = %v, want %v",
					tt.rule, tt.toolName, tt.arg, result, tt.expected)
			}
		})
	}
}

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern, s string
		expected   bool
	}{
		// Exact match
		{"hello", "hello", true},
		{"hello", "world", false},

		// Empty patterns
		{"", "", true},
		{"", "something", false},

		// Single star
		{"*", "anything", true},
		{"*", "", true},
		{"hello*", "hello world", true},
		{"hello*", "hello", true},
		{"hello*", "hell", false},
		{"*world", "hello world", true},

		// Multiple stars - globMatch allows / in *
		{"*/*.go", "pkg/main.go", true},
		{"*/*.go", "main.go", false}, // no slash, doesn't match */*.go

		// Complex patterns
		{"git *", "git status", true},
		{"git *", "git commit -m \"message\"", true},
		{"git *", "npm install", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.s, func(t *testing.T) {
			result := globMatch(tt.pattern, tt.s)
			if result != tt.expected {
				t.Errorf("globMatch(%q, %q) = %v, want %v",
					tt.pattern, tt.s, result, tt.expected)
			}
		})
	}
}
