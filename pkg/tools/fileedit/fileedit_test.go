package fileedit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
