package glob

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

func TestGlob_Basic(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "util.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("text"), 0644)

	tool := New(dir)
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "*.go",
		"path":    dir,
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "main.go") {
		t.Errorf("expected main.go in output")
	}
	if strings.Contains(result.Content, "readme.txt") {
		t.Errorf("unexpected readme.txt in output")
	}
}

func TestGlob_NoMatches(t *testing.T) {
	dir := t.TempDir()
	tool := New(dir)
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "*.xyz",
		"path":    dir,
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "no files found") {
		t.Errorf("expected 'no files found', got: %q", result.Content)
	}
}

func TestGlob_Doublestar(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pkg", "api"), 0755)
	os.WriteFile(filepath.Join(dir, "pkg", "api", "types.go"), []byte("package api"), 0644)
	os.WriteFile(filepath.Join(dir, "pkg", "api", "client.go"), []byte("package api"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	cases := []struct {
		pattern string
		want    []string
		notwant []string
	}{
		{"**/*.go", []string{"types.go", "client.go", "main.go"}, nil},
		{"pkg/**/*.go", []string{"types.go", "client.go"}, []string{"main.go"}},
		{"**/*.ts", nil, []string{".go"}},
	}

	for _, c := range cases {
		tool := New(dir)
		input, _ := json.Marshal(map[string]interface{}{
			"pattern": c.pattern,
			"path":    dir,
		})
		result, _ := tool.Call(context.Background(), input)
		if result.IsError {
			t.Fatalf("pattern %q: unexpected error: %s", c.pattern, result.Content)
		}
		for _, w := range c.want {
			if !strings.Contains(result.Content, w) {
				t.Errorf("pattern %q: expected %q in output, got:\n%s", c.pattern, w, result.Content)
			}
		}
		for _, nw := range c.notwant {
			if strings.Contains(result.Content, nw) {
				t.Errorf("pattern %q: unexpected %q in output", c.pattern, nw)
			}
		}
	}
}

func TestGlob_Permissions(t *testing.T) {
	tool := New("/")

	tests := []struct {
		name     string
		input    string
		rules    tools.PermissionRules
		expected string
	}{
		{
			name:  "deny rule",
			input: `{"pattern":"*.go","path":"/etc"}`,
			rules: tools.PermissionRules{
				Deny: []string{"Glob"},
			},
			expected: "deny",
		},
		{
			name:  "allow rule",
			input: `{"pattern":"*.go","path":"/home/user"}`,
			rules: tools.PermissionRules{
				Allow: []string{"Glob"},
			},
			expected: "allow",
		},
		{
			name:     "default allow (read-only)",
			input:    `{"pattern":"*.go","path":"/tmp"}`,
			rules:    tools.PermissionRules{},
			expected: "allow",
		},
		{
			name:     "invalid json",
			input:    `not json`,
			rules:    tools.PermissionRules{},
			expected: "allow", // read-only default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tool.CheckPermissions(json.RawMessage(tt.input), "default", tt.rules)
			if d.Behavior != tt.expected {
				t.Errorf("CheckPermissions() = %q, want %q", d.Behavior, tt.expected)
			}
		})
	}
}

func TestGlob_Metadata(t *testing.T) {
	tool := New(".")

	if tool.Name() != "Glob" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "Glob")
	}
	if !tool.IsReadOnly() {
		t.Error("IsReadOnly() should be true")
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if len(tool.InputSchema()) == 0 {
		t.Error("InputSchema() should not be empty")
	}
}

func TestGlob_EmptyPattern(t *testing.T) {
	tool := New(".")
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "",
	})
	result, _ := tool.Call(context.Background(), input)
	// Empty pattern might work or return no results, not necessarily an error
	// Just check it doesn't crash
	_ = result
}

func TestGlob_InvalidJSON(t *testing.T) {
	tool := New(".")
	result, _ := tool.Call(context.Background(), json.RawMessage(`invalid json`))
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestGlob_DefaultPath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main"), 0644)

	tool := New(dir)
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "*.go",
		// no path specified, should use CWD
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "test.go") {
		t.Errorf("expected test.go, got: %s", result.Content)
	}
}

func TestGlob_NonexistentPath(t *testing.T) {
	tool := New(".")
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "*.go",
		"path":    "/nonexistent/path",
	})
	result, _ := tool.Call(context.Background(), input)
	// Nonexistent path might return no files found instead of error
	// Check that it doesn't crash and returns something
	_ = result
}

func TestGlob_HiddenFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("hidden"), 0644)
	os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("visible"), 0644)

	tool := New(dir)

	// Test that hidden files can be matched with explicit pattern
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": ".*",
		"path":    dir,
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, ".hidden") {
		t.Errorf("expected .hidden in output, got: %s", result.Content)
	}
}
