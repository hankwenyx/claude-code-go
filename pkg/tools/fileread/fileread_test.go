package fileread

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
