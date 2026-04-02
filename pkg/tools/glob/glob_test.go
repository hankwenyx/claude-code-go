package glob

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
