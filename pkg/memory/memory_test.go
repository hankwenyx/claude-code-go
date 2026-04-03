package memory_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hankwenyx/claude-code-go/pkg/memory"
)

// ---- TruncateMemory ----

func TestTruncateMemory_NoTruncation(t *testing.T) {
	content := "- item 1\n- item 2\n"
	got := memory.TruncateMemory(content)
	if got != content {
		t.Errorf("expected no change, got %q", got)
	}
}

func TestTruncateMemory_LineTruncation(t *testing.T) {
	// Build 250 lines (> MaxMemoryLines=200)
	var sb strings.Builder
	for i := 0; i < 250; i++ {
		sb.WriteString("- line\n")
	}
	got := memory.TruncateMemory(sb.String())
	count := strings.Count(got, "\n")
	if count > memory.MaxMemoryLines {
		t.Errorf("expected ≤%d lines, got %d", memory.MaxMemoryLines, count)
	}
}

func TestTruncateMemory_ByteTruncation(t *testing.T) {
	// Build content that exceeds MaxMemoryBytes (25KB)
	var sb strings.Builder
	for len(sb.String()) < memory.MaxMemoryBytes+1000 {
		sb.WriteString("- " + strings.Repeat("x", 100) + "\n")
	}
	got := memory.TruncateMemory(sb.String())
	if len(got) > memory.MaxMemoryBytes {
		t.Errorf("expected ≤%d bytes, got %d", memory.MaxMemoryBytes, len(got))
	}
}

func TestTruncateMemory_Empty(t *testing.T) {
	if got := memory.TruncateMemory(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ---- CWDSlug ----

func TestCWDSlug(t *testing.T) {
	cases := []struct{ input, want string }{
		{"/home/user/project", "home-user-project"},
		{"/Users/bob/go/src/myapp", "Users-bob-go-src-myapp"},
		{"C:\\Users\\bob\\project", "C-Users-bob-project"}, // Windows path: : removed, \ → -
		{"relative/path", "relative-path"},
	}
	for _, tc := range cases {
		got := memory.CWDSlug(tc.input)
		if got != tc.want {
			t.Errorf("CWDSlug(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---- AppendMemory / LoadMemory ----

func TestAppendMemory_CreateNew(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	err := memory.AppendMemory(cwd, "- first memory\n- second memory")
	if err != nil {
		t.Fatalf("AppendMemory: %v", err)
	}

	content, err := memory.LoadMemory(cwd)
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if !strings.Contains(content, "first memory") {
		t.Errorf("expected 'first memory' in %q", content)
	}
	if !strings.Contains(content, "second memory") {
		t.Errorf("expected 'second memory' in %q", content)
	}
}

func TestAppendMemory_Accumulates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	memory.AppendMemory(cwd, "- alpha")
	memory.AppendMemory(cwd, "- beta")

	content, err := memory.LoadMemory(cwd)
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if !strings.Contains(content, "alpha") || !strings.Contains(content, "beta") {
		t.Errorf("both entries should be present: %q", content)
	}
}

func TestLoadMemory_NotExist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	content, err := memory.LoadMemory(cwd)
	if err != nil {
		t.Fatalf("LoadMemory on missing file: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty, got %q", content)
	}
}

// ---- MemoryPath ----

func TestMemoryPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/tmp/myproject"
	path, err := memory.MemoryPath(cwd)
	if err != nil {
		t.Fatal(err)
	}
	slug := memory.CWDSlug(cwd)
	expected := filepath.Join(home, ".claude", "projects", slug, "memory", "MEMORY.md")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
}

func TestAppendMemory_TruncatesOnOverflow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()

	// Write many lines to trigger truncation
	for i := 0; i < 5; i++ {
		var sb strings.Builder
		for j := 0; j < 50; j++ {
			sb.WriteString("- memory item\n")
		}
		memory.AppendMemory(cwd, sb.String())
	}

	// Verify file exists and is within limits
	path, _ := memory.MemoryPath(cwd)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(data) > memory.MaxMemoryBytes+100 { // +100 for atomic tmp overhead
		t.Errorf("file too large: %d bytes", len(data))
	}
	lines := strings.Count(string(data), "\n")
	if lines > memory.MaxMemoryLines+5 {
		t.Errorf("too many lines: %d", lines)
	}
}
