package fileedit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
	"github.com/hankwenyx/claude-code-go/pkg/tools/fileread"
)

func readFile(t *testing.T, store *fileread.StateStore, path string) {
	t.Helper()
	tool := fileread.New(store)
	input, _ := json.Marshal(map[string]interface{}{"file_path": path})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("read failed: %s", result.Content)
	}
}

func TestFileEdit_Basic(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("hello world"), 0644)

	store := fileread.NewStateStore()
	readFile(t, store, f)

	tool := New(store)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path":  f,
		"old_string": "hello",
		"new_string": "goodbye",
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	data, _ := os.ReadFile(f)
	if !strings.Contains(string(data), "goodbye") {
		t.Errorf("expected 'goodbye' in file, got: %q", string(data))
	}
}

func TestFileEdit_RequiresRead(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("hello world"), 0644)

	store := fileread.NewStateStore()
	// NOT reading the file first
	tool := New(store)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path":  f,
		"old_string": "hello",
		"new_string": "goodbye",
	})
	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error when file not read first")
	}
	if !strings.Contains(result.Content, "not been read") {
		t.Errorf("expected 'not been read' error, got: %q", result.Content)
	}
}

func TestFileEdit_NoChange(t *testing.T) {
	tool := New(nil)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path":  "/any",
		"old_string": "same",
		"new_string": "same",
	})
	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error for no-change edit")
	}
}

func TestFileEdit_MultipleMatches(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("foo foo foo"), 0644)

	store := fileread.NewStateStore()
	readFile(t, store, f)

	tool := New(store)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path":  f,
		"old_string": "foo",
		"new_string": "bar",
	})
	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error for multiple matches without replace_all")
	}
}

func TestFileEdit_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("foo foo foo"), 0644)

	store := fileread.NewStateStore()
	readFile(t, store, f)

	tool := New(store)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path":   f,
		"old_string":  "foo",
		"new_string":  "bar",
		"replace_all": true,
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "bar bar bar" {
		t.Errorf("expected 'bar bar bar', got: %q", string(data))
	}
}

func TestFileEdit_Permissions(t *testing.T) {
	store := fileread.NewStateStore()
	tool := New(store)

	tests := []struct {
		name     string
		input    string
		rules    tools.PermissionRules
		expected string
	}{
		{
			name:  "deny rule",
			input: `{"file_path":"/etc/passwd","old_string":"x","new_string":"y"}`,
			rules: tools.PermissionRules{
				Deny: []string{"Edit(/etc/*)"},
			},
			expected: "deny",
		},
		{
			name:  "deny rule matches",
			input: `{"file_path":"/etc/shadow","old_string":"x","new_string":"y"}`,
			rules: tools.PermissionRules{
				Deny: []string{"Edit"},
			},
			expected: "deny",
		},
		{
			name:  "ask rule",
			input: `{"file_path":"/home/user/file.txt","old_string":"x","new_string":"y"}`,
			rules: tools.PermissionRules{
				Ask: []string{"Edit"},
			},
			expected: "ask",
		},
		{
			name:     "no matching rule - default ask",
			input:    `{"file_path":"/home/user/file.txt","old_string":"x","new_string":"y"}`,
			rules:    tools.PermissionRules{},
			expected: "ask",
		},
		{
			name:     "invalid json",
			input:    `not json`,
			rules:    tools.PermissionRules{},
			expected: "deny",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tool.CheckPermissions(json.RawMessage(tt.input), "default", tt.rules)
			if d.Behavior != tt.expected {
				t.Errorf("CheckPermissions() = %q, want %q (reason: %s)", d.Behavior, tt.expected, d.Reason)
			}
		})
	}
}

func TestFileEdit_Metadata(t *testing.T) {
	tool := New(nil)

	if tool.Name() != "Edit" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "Edit")
	}
	if tool.IsReadOnly() {
		t.Error("IsReadOnly() should be false")
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if len(tool.InputSchema()) == 0 {
		t.Error("InputSchema() should not be empty")
	}
}

func TestFileEdit_FileNotExist(t *testing.T) {
	store := fileread.NewStateStore()
	tool := New(store)

	input, _ := json.Marshal(map[string]interface{}{
		"file_path":   "/nonexistent/file.txt",
		"old_string":  "x",
		"new_string":  "y",
	})

	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error for non-existent file")
	}
}

func TestFileEdit_InvalidJSON(t *testing.T) {
	tool := New(nil)
	result, _ := tool.Call(context.Background(), json.RawMessage(`invalid json`))
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestFileEdit_EmptyFilePath(t *testing.T) {
	tool := New(nil)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path":   "",
		"old_string":  "x",
		"new_string":  "y",
	})
	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error for empty file_path")
	}
}

func TestFileEdit_OldStringNotFound(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("hello world"), 0644)

	store := fileread.NewStateStore()
	readFile(t, store, f)

	tool := New(store)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path":   f,
		"old_string":  "nonexistent",
		"new_string":  "replacement",
	})

	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error when old_string not found")
	}
}

func TestFileEdit_StateTracking(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("hello world"), 0644)

	store := fileread.NewStateStore()
	readFile(t, store, f)

	// Check state was tracked
	state, ok := store.Get(f)
	if !ok {
		t.Fatal("expected file state to be tracked after read")
	}

	// Edit the file
	tool := New(store)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path":  f,
		"old_string": "hello",
		"new_string": "goodbye",
	})
	tool.Call(context.Background(), input)

	// State should be updated
	newState, ok := store.Get(f)
	if !ok {
		t.Fatal("expected file state to still be tracked")
	}
	if newState.MTime.Before(state.MTime) {
		t.Error("expected MTime to be updated after edit")
	}
}

func TestFileEdit_CheckPermissions_MatchRule(t *testing.T) {
	tool := New(nil)

	tests := []struct {
		name     string
		path     string
		rules    tools.PermissionRules
		expected string
	}{
		{
			name: "deny /etc/*",
			path: "/etc/passwd",
			rules: tools.PermissionRules{
				Deny: []string{"Edit(/etc/*)"},
			},
			expected: "deny",
		},
		{
			name: "deny tool name",
			path: "/any/path/file.txt",
			rules: tools.PermissionRules{
				Deny: []string{"Edit"},
			},
			expected: "deny",
		},
		{
			name: "ask tool name",
			path: "/home/user/file.txt",
			rules: tools.PermissionRules{
				Ask: []string{"Edit"},
			},
			expected: "ask",
		},
		{
			name:     "no rules - default ask",
			path:     "/tmp/file.txt",
			rules:    tools.PermissionRules{},
			expected: "ask",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := json.Marshal(map[string]interface{}{
				"file_path":   tt.path,
				"old_string":  "x",
				"new_string":  "y",
			})
			d := tool.CheckPermissions(input, "default", tt.rules)
			if d.Behavior != tt.expected {
				t.Errorf("path %q: got %q, want %q", tt.path, d.Behavior, tt.expected)
			}
		})
	}
}
