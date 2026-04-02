package grep

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

func TestGrep_ContentMode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\nfunc hello() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main\nfunc world() {}\n"), 0644)

	tool := New(dir)
	input, _ := json.Marshal(map[string]interface{}{
		"pattern":     "hello",
		"path":        dir,
		"output_mode": "content",
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Errorf("expected 'hello' in output, got: %q", result.Content)
	}
	if strings.Contains(result.Content, "world") {
		t.Errorf("unexpected 'world' in output")
	}
}

func TestGrep_FilesWithMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "match.go"), []byte("target string here\n"), 0644)
	os.WriteFile(filepath.Join(dir, "nomatch.go"), []byte("nothing here\n"), 0644)

	tool := New(dir)
	input, _ := json.Marshal(map[string]interface{}{
		"pattern":     "target",
		"path":        dir,
		"output_mode": "files_with_matches",
	})
	result, _ := tool.Call(context.Background(), input)
	if !strings.Contains(result.Content, "match.go") {
		t.Errorf("expected match.go in output, got: %q", result.Content)
	}
	if strings.Contains(result.Content, "nomatch.go") {
		t.Errorf("unexpected nomatch.go in output")
	}
}

func TestGrep_NoMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("nothing here\n"), 0644)

	tool := New(dir)
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "zzznomatch",
		"path":    dir,
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "no matches found") {
		t.Errorf("expected 'no matches found', got: %q", result.Content)
	}
}

func TestGrep_CountMode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello\nhello world\nsay hello\n"), 0644)

	tool := New(dir)
	input, _ := json.Marshal(map[string]interface{}{
		"pattern":     "hello",
		"path":        dir,
		"output_mode": "count",
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	// Should count occurrences
	if result.Content == "" {
		t.Error("expected count output")
	}
}

func TestGrep_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("HELLO\nHello\nhello\n"), 0644)

	tool := New(dir)
	input, _ := json.Marshal(map[string]interface{}{
		"pattern":     "hello",
		"path":        dir,
		"output_mode": "count",
		"-i":          true,
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	// All three should match with case insensitive
	if result.Content == "0" {
		t.Error("expected matches with case insensitive")
	}
}

func TestGrep_GlobFilter(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("target found\n"), 0644)
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("target not shown\n"), 0644)

	tool := New(dir)
	input, _ := json.Marshal(map[string]interface{}{
		"pattern":     "target",
		"path":        dir,
		"glob":        "*.go",
		"output_mode": "files_with_matches",
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "test.go") {
		t.Errorf("expected test.go, got: %q", result.Content)
	}
	if strings.Contains(result.Content, "test.txt") {
		t.Errorf("test.txt should be filtered out by glob")
	}
}

func TestGrep_EmptyPattern(t *testing.T) {
	tool := New(".")
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "",
	})
	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error for empty pattern")
	}
	if !strings.Contains(result.Content, "pattern is required") {
		t.Errorf("unexpected error: %s", result.Content)
	}
}

func TestGrep_InvalidJSON(t *testing.T) {
	tool := New(".")
	result, _ := tool.Call(context.Background(), json.RawMessage(`not json`))
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestGrep_DefaultPath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("content\n"), 0644)

	tool := New(dir) // CWD set to temp dir
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "content",
		// no path specified, should use CWD
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "test.txt") {
		t.Errorf("expected test.txt in output, got: %q", result.Content)
	}
}

func TestGrep_SkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	os.MkdirAll(gitDir, 0755)
	os.WriteFile(filepath.Join(gitDir, "config"), []byte("secret=value\n"), 0644)
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("public content\n"), 0644)

	tool := New(dir)
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "secret",
		"path":    dir,
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	// .git directory should be skipped
	if strings.Contains(result.Content, ".git") {
		t.Errorf(".git directory should be skipped, got: %q", result.Content)
	}
}

func TestGrep_Permissions_DefaultAllow(t *testing.T) {
	tool := New(".")
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "test",
	})
	d := tool.CheckPermissions(input, "default", tools.PermissionRules{})
	if d.Behavior != "allow" {
		t.Errorf("expected default allow for read-only tool, got %q", d.Behavior)
	}
}

func TestGrep_Permissions_Deny(t *testing.T) {
	tool := New(".")
	input, _ := json.Marshal(map[string]interface{}{
		"pattern": "test",
		"path":    "/secret/dir",
	})
	d := tool.CheckPermissions(input, "default", tools.PermissionRules{
		Deny: []string{"Grep(/secret/*)"},
	})
	if d.Behavior != "deny" {
		t.Errorf("expected deny, got %q", d.Behavior)
	}
}

func TestGrep_Metadata(t *testing.T) {
	tool := New(".")

	if tool.Name() != "Grep" {
		t.Errorf("Name: got %q", tool.Name())
	}
	if !tool.IsReadOnly() {
		t.Error("IsReadOnly should be true")
	}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
	if len(tool.InputSchema()) == 0 {
		t.Error("InputSchema should not be empty")
	}
}
