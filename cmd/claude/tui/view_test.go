package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandAtMentions(t *testing.T) {
	dir := t.TempDir()

	// Create test files
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("file content here"), 0644)
	os.WriteFile(filepath.Join(dir, "code.go"), []byte("package main\n\nfunc main() {}"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir, "subdir", "nested.md"), []byte("# Nested"), 0644)

	tests := []struct {
		name     string
		input    string
		contains string
		notContains string
	}{
		{
			name:     "no mentions",
			input:    "hello world",
			contains: "hello world",
		},
		{
			name:     "single mention",
			input:    "read @test.txt",
			contains: "<file_content path=\"test.txt\">",
		},
		{
			name:     "mention with content",
			input:    "@test.txt",
			contains: "file content here",
		},
		{
			name:     "nonexistent file",
			input:    "@nonexistent.txt",
			contains: "@nonexistent.txt[file not found]",
		},
		{
			name:     "relative path",
			input:    "@./test.txt",
			contains: "<file_content path=\"./test.txt\">",
		},
		{
			name:     "nested path",
			input:    "@subdir/nested.md",
			contains: "# Nested",
		},
		{
			name:     "multiple mentions",
			input:    "compare @test.txt and @code.go",
			contains: "file content here",
			notContains: "@test.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandAtMentions(tt.input, dir)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("expandAtMentions(%q) = %q, want to contain %q", tt.input, got, tt.contains)
			}
			if tt.notContains != "" && strings.Contains(got, tt.notContains) {
				t.Errorf("expandAtMentions(%q) = %q, should not contain %q", tt.input, got, tt.notContains)
			}
		})
	}
}

func TestTruncate80(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "short string",
			input: "short",
			want:  "short",
		},
		{
			name:  "exactly 80 chars",
			input: strings.Repeat("x", 80),
			want:  strings.Repeat("x", 80),
		},
		{
			name:  "over 80 chars",
			input: strings.Repeat("x", 100),
			want:  strings.Repeat("x", 77) + "...",
		},
		{
			name:  "multiline converted to single line",
			input: "line1\nline2\nline3",
			want:  "line1 line2 line3",
		},
		{
			name:  "multiline over 80 chars",
			input: strings.Repeat("abc\n", 30),
			want:  strings.Repeat("abc ", 19) + "a...",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate80(tt.input)
			if got != tt.want {
				t.Errorf("truncate80(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummaryFromInput(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]interface{}
		want     string
	}{
		{
			name:     "Bash command",
			toolName: "Bash",
			input:    map[string]interface{}{"command": "git status"},
			want:     "git status",
		},
		{
			name:     "Read file_path",
			toolName: "Read",
			input:    map[string]interface{}{"file_path": "/tmp/file.txt"},
			want:     "/tmp/file.txt",
		},
		{
			name:     "Write file_path",
			toolName: "Write",
			input:    map[string]interface{}{"file_path": "/tmp/output.txt"},
			want:     "/tmp/output.txt",
		},
		{
			name:     "Edit file_path",
			toolName: "Edit",
			input:    map[string]interface{}{"file_path": "/src/main.go"},
			want:     "/src/main.go",
		},
		{
			name:     "Glob pattern",
			toolName: "Glob",
			input:    map[string]interface{}{"pattern": "**/*.go"},
			want:     "**/*.go",
		},
		{
			name:     "Grep pattern",
			toolName: "Grep",
			input:    map[string]interface{}{"pattern": "func Test"},
			want:     "func Test",
		},
		{
			name:     "WebFetch url",
			toolName: "WebFetch",
			input:    map[string]interface{}{"url": "https://example.com"},
			want:     "https://example.com",
		},
		{
			name:     "Unknown tool returns first string value",
			toolName: "Unknown",
			input:    map[string]interface{}{"some_key": "some_value"},
			want:     "some_value",
		},
		{
			name:     "Empty input",
			toolName: "Bash",
			input:    map[string]interface{}{},
			want:     "",
		},
		{
			name:     "Long command truncated",
			toolName: "Bash",
			input:    map[string]interface{}{"command": strings.Repeat("x", 100)},
			want:     strings.Repeat("x", 77) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputJSON, _ := json.Marshal(tt.input)
			got := toolSummaryFromInput(tt.toolName, inputJSON)
			if got != tt.want {
				t.Errorf("toolSummaryFromInput(%q, %s) = %q, want %q", tt.toolName, inputJSON, got, tt.want)
			}
		})
	}
}

func TestToolSummaryFromInput_InvalidJSON(t *testing.T) {
	got := toolSummaryFromInput("Bash", []byte("not valid json"))
	if got != "" {
		t.Errorf("toolSummaryFromInput with invalid JSON = %q, want empty string", got)
	}
}

func TestToolSummaryFromInput_NestedValue(t *testing.T) {
	// Test with nested object - should return empty as it's not a string
	input := map[string]interface{}{
		"command": map[string]interface{}{"nested": "value"},
	}
	inputJSON, _ := json.Marshal(input)
	got := toolSummaryFromInput("Bash", inputJSON)
	if got != "" {
		t.Errorf("toolSummaryFromInput with nested object = %q, want empty string", got)
	}
}

func TestRenderStatusLine(t *testing.T) {
	tests := []struct {
		name      string
		left      string
		right     string
		width     int
		wantLeft  string
		wantRight string
	}{
		{
			name:      "normal case",
			left:      "Running...",
			right:     "claude-sonnet-4-6",
			width:     40,
			wantLeft:  "Running...",
			wantRight: "claude-sonnet-4-6",
		},
		{
			name:      "narrow width",
			left:      "Status",
			right:     "Info",
			width:     15,
			wantLeft:  "Status",
			wantRight: "Info",
		},
		{
			name:      "empty strings",
			left:      "",
			right:     "",
			width:     20,
			wantLeft:  "",
			wantRight: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderStatusLine(tt.left, tt.right, tt.width)
			if !strings.HasPrefix(got, tt.wantLeft) {
				t.Errorf("renderStatusLine left = %q, want prefix %q", got, tt.wantLeft)
			}
			if !strings.HasSuffix(got, tt.wantRight) {
				t.Errorf("renderStatusLine right = %q, want suffix %q", got, tt.wantRight)
			}
		})
	}
}
