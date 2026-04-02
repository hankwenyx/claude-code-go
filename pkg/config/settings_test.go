package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeStringSlice(t *testing.T) {
	a := []string{"a", "b", "c"}
	b := []string{"b", "d"}
	got := mergeStringSlice(a, b)
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q want %q", i, got[i], want[i])
		}
	}
}

func TestMergeSettings_ModelOverride(t *testing.T) {
	layers := []SettingsJson{
		{Model: "claude-haiku"},
		{Model: "claude-sonnet-4-6"},
	}
	m := mergeSettings(layers)
	if m.Model != "claude-sonnet-4-6" {
		t.Errorf("got %q want %q", m.Model, "claude-sonnet-4-6")
	}
}

func TestMergeSettings_PermissionsAppend(t *testing.T) {
	layers := []SettingsJson{
		{Permissions: PermissionsSettings{Allow: []string{"Bash(git *)"}}},
		{Permissions: PermissionsSettings{Allow: []string{"Read"}}},
	}
	m := mergeSettings(layers)
	if len(m.Permissions.Allow) != 2 {
		t.Fatalf("got %d allow rules, want 2", len(m.Permissions.Allow))
	}
}

func TestLoadSettings_UserFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	content := `{"model":"claude-test","permissions":{"allow":["Bash"]}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadSettings(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "claude-test" {
		t.Errorf("model: got %q want %q", s.Model, "claude-test")
	}
	if len(s.Permissions.Allow) != 1 || s.Permissions.Allow[0] != "Bash" {
		t.Errorf("allow rules: got %v", s.Permissions.Allow)
	}
}
