package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStripHTMLComments(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello <!-- comment --> world", "hello  world"},
		{"no comments here", "no comments here"},
		{"<!-- multi\nline -->end", "end"},
	}
	for _, c := range cases {
		got := stripHTMLComments(c.in)
		if got != c.want {
			t.Errorf("stripHTMLComments(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLoadClaudeMds_BasicFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	content := "# Project Instructions\n\nDo X and Y."
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := LoadClaudeMds(dir)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, f := range files {
		if f.Type == ClaudeMdTypeUser {
			found = true
			if f.Content != content {
				t.Errorf("content mismatch: got %q", f.Content)
			}
		}
	}
	if !found {
		t.Error("user CLAUDE.md not found")
	}
}

func TestFormatClaudeMdMessage(t *testing.T) {
	files := []ClaudeMdFile{
		{Path: "/proj/CLAUDE.md", Content: "Do X.", Type: ClaudeMdTypeProject},
	}
	msg := FormatClaudeMdMessage(files, "2026-04-02")
	if msg == "" {
		t.Error("expected non-empty message")
	}
	if !contains(msg, "Do X.") {
		t.Error("expected content in message")
	}
	if !contains(msg, "2026-04-02") {
		t.Error("expected date in message")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
