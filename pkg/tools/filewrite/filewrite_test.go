package filewrite

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

func TestFileWrite_Basic(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")

	store := fileread.NewStateStore()
	tool := New(store)

	input, _ := json.Marshal(map[string]interface{}{
		"file_path": f,
		"content":   "hello world",
	})
	result, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	// Verify file was written
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("content: got %q, want %q", string(data), "hello world")
	}

	// Verify success message
	if !strings.Contains(result.Content, "wrote") {
		t.Errorf("expected 'wrote' in result, got: %s", result.Content)
	}
}

func TestFileWrite_CreateParentDirs(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "subdir", "deep", "test.txt")

	tool := New(nil)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path": f,
		"content":   "nested content",
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	// Verify file and directories were created
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("failed to read nested file: %v", err)
	}
	if string(data) != "nested content" {
		t.Errorf("content: got %q", string(data))
	}
}

func TestFileWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")

	// Create initial file
	os.WriteFile(f, []byte("original"), 0644)

	tool := New(nil)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path": f,
		"content":   "new content",
	})
	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	data, _ := os.ReadFile(f)
	if string(data) != "new content" {
		t.Errorf("content: got %q, want %q", string(data), "new content")
	}
}

func TestFileWrite_EmptyPath(t *testing.T) {
	tool := New(nil)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path": "",
		"content":   "test",
	})
	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error for empty file_path")
	}
	if !strings.Contains(result.Content, "file_path is required") {
		t.Errorf("unexpected error message: %s", result.Content)
	}
}

func TestFileWrite_InvalidInput(t *testing.T) {
	tool := New(nil)
	result, _ := tool.Call(context.Background(), json.RawMessage(`not valid json`))
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestFileWrite_StateTracking(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "tracked.txt")

	store := fileread.NewStateStore()
	tool := New(store)

	input, _ := json.Marshal(map[string]interface{}{
		"file_path": f,
		"content":   "tracked content",
	})
	tool.Call(context.Background(), input)

	// Verify state was updated
	state, ok := store.Get(f)
	if !ok {
		t.Fatal("expected file state to be tracked")
	}
	if state.MTime.IsZero() {
		t.Error("expected MTime to be set")
	}
}

func TestFileWrite_Permissions_Allow(t *testing.T) {
	tool := New(nil)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path": "/tmp/test.txt",
		"content":   "test",
	})
	d := tool.CheckPermissions(input, "default", tools.PermissionRules{
		Allow: []string{"Write(/tmp/**)"},
	})
	if d.Behavior != "allow" {
		t.Errorf("expected allow, got %q: %s", d.Behavior, d.Reason)
	}
}

func TestFileWrite_Permissions_Deny(t *testing.T) {
	tool := New(nil)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path": "/etc/passwd",
		"content":   "malicious",
	})
	d := tool.CheckPermissions(input, "default", tools.PermissionRules{
		Deny: []string{"Write(/etc/*)"},
	})
	if d.Behavior != "deny" {
		t.Errorf("expected deny, got %q", d.Behavior)
	}
}

func TestFileWrite_Permissions_Ask(t *testing.T) {
	tool := New(nil)
	input, _ := json.Marshal(map[string]interface{}{
		"file_path": "/home/user/file.txt",
		"content":   "test",
	})
	d := tool.CheckPermissions(input, "default", tools.PermissionRules{})
	if d.Behavior != "ask" {
		t.Errorf("expected ask (default), got %q", d.Behavior)
	}
}

func TestFileWrite_Permissions_InvalidInput(t *testing.T) {
	tool := New(nil)
	d := tool.CheckPermissions(json.RawMessage(`invalid`), "default", tools.PermissionRules{})
	if d.Behavior != "deny" {
		t.Errorf("expected deny for invalid input, got %q", d.Behavior)
	}
}

func TestFileWrite_Metadata(t *testing.T) {
	tool := New(nil)

	if tool.Name() != "Write" {
		t.Errorf("Name: got %q", tool.Name())
	}
	if tool.IsReadOnly() {
		t.Error("IsReadOnly should be false")
	}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
	if len(tool.InputSchema()) == 0 {
		t.Error("InputSchema should not be empty")
	}
}
