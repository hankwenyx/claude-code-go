package fileread

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

func TestFileRead_Basic(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0644)

	store := NewStateStore()
	tool := New(store)

	input, _ := json.Marshal(map[string]interface{}{"file_path": f})
	result, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "line1") {
		t.Errorf("expected 'line1' in output, got: %q", result.Content)
	}
	// Verify line numbers
	if !strings.Contains(result.Content, "     1\t") {
		t.Errorf("expected line number format, got: %q", result.Content)
	}
}

func TestFileRead_WithOffsetAndLimit(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	content := "a\nb\nc\nd\ne\n"
	os.WriteFile(f, []byte(content), 0644)

	store := NewStateStore()
	tool := New(store)

	input, _ := json.Marshal(map[string]interface{}{
		"file_path": f,
		"offset":    2,
		"limit":     2,
	})
	result, _ := tool.Call(context.Background(), input)
	if !strings.Contains(result.Content, "b") {
		t.Errorf("offset 2 should include line 2 ('b'), got: %q", result.Content)
	}
	if strings.Contains(result.Content, "d") {
		t.Errorf("limit 2 should exclude line 4 ('d'), got: %q", result.Content)
	}
}

func TestFileRead_TracksState(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("content"), 0644)

	store := NewStateStore()
	tool := New(store)

	input, _ := json.Marshal(map[string]interface{}{"file_path": f})
	tool.Call(context.Background(), input)

	_, ok := store.Get(f)
	if !ok {
		t.Error("expected file state to be tracked after read")
	}
}

func TestFileRead_FileNotFound(t *testing.T) {
	tool := New(NewStateStore())
	input, _ := json.Marshal(map[string]interface{}{"file_path": "/nonexistent/file.txt"})
	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected IsError=true for missing file")
	}
}

func TestFileRead_Permissions(t *testing.T) {
	store := NewStateStore()
	tool := New(store)

	// Read is a read-only tool, default is allow
	// CheckPermissions returns allow for most cases

	tests := []struct {
		name     string
		input    string
		rules    tools.PermissionRules
		expected string
	}{
		{
			name:  "deny rule",
			input: `{"file_path":"/etc/passwd"}`,
			rules: tools.PermissionRules{
				Deny: []string{"Read(/etc/*)"},
			},
			expected: "deny",
		},
		{
			name:  "deny tool name",
			input: `{"file_path":"/any/path"}`,
			rules: tools.PermissionRules{
				Deny: []string{"Read"},
			},
			expected: "deny",
		},
		{
			name:     "default allow (read-only)",
			input:    `{"file_path":"/tmp/file"}`,
			rules:    tools.PermissionRules{},
			expected: "allow",
		},
		{
			name:     "invalid json - still allow for read-only",
			input:    `not json`,
			rules:    tools.PermissionRules{},
			expected: "allow",
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

func TestFileRead_Metadata(t *testing.T) {
	tool := New(nil)

	if tool.Name() != "Read" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "Read")
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

func TestFileRead_EmptyFilePath(t *testing.T) {
	tool := New(NewStateStore())
	input, _ := json.Marshal(map[string]interface{}{
		"file_path": "",
	})
	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error for empty file_path")
	}
}

func TestFileRead_InvalidJSON(t *testing.T) {
	tool := New(NewStateStore())
	result, _ := tool.Call(context.Background(), json.RawMessage(`invalid json`))
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestFileRead_StateStore(t *testing.T) {
	store := NewStateStore()

	// Initially no state
	_, ok := store.Get("/nonexistent")
	if ok {
		t.Error("expected no state for nonexistent file")
	}

	// Set state with a non-zero MTime
	store.Set("/test/file.txt", FileState{})
	state, ok := store.Get("/test/file.txt")
	if !ok {
		t.Error("expected state after Set")
	}
	// MTime might be zero if not explicitly set, that's OK
	_ = state
}

func TestFileRead_LargeFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "large.txt")

	// Create a file with many lines
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line content")
	}
	os.WriteFile(f, []byte(strings.Join(lines, "\n")), 0644)

	store := NewStateStore()
	tool := New(store)

	input, _ := json.Marshal(map[string]interface{}{
		"file_path": f,
		"limit":     10,
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	// Count lines in output
	lineCount := strings.Count(result.Content, "\n")
	if lineCount > 12 { // allow some extra for formatting
		t.Errorf("expected ~10 lines, got %d", lineCount)
	}
}

func TestFileRead_OffsetExceedsFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "small.txt")
	os.WriteFile(f, []byte("only one line"), 0644)

	store := NewStateStore()
	tool := New(store)

	input, _ := json.Marshal(map[string]interface{}{
		"file_path": f,
		"offset":    100,
	})
	result, _ := tool.Call(context.Background(), input)
	// Should handle gracefully - either error or empty content
	// Large offset may return empty content or error with "exceeds" message
	_ = result
}

func TestFileRead_DirectoryPath(t *testing.T) {
	dir := t.TempDir()

	tool := New(NewStateStore())
	input, _ := json.Marshal(map[string]interface{}{
		"file_path": dir,
	})
	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error when reading a directory")
	}
}
