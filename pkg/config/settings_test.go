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

// --- new coverage tests ---

func TestParsedModel_NoSuffix(t *testing.T) {
	m := &MergedSettings{Model: "claude-sonnet-4-6"}
	name, tokens := m.ParsedModel()
	if name != "claude-sonnet-4-6" || tokens != 0 {
		t.Errorf("got %q %d", name, tokens)
	}
}

func TestParsedModel_KSuffix(t *testing.T) {
	m := &MergedSettings{Model: "claude-opus-4-6[200k]"}
	name, tokens := m.ParsedModel()
	if name != "claude-opus-4-6" || tokens != 200_000 {
		t.Errorf("got %q %d", name, tokens)
	}
}

func TestParsedModel_MSuffix(t *testing.T) {
	m := &MergedSettings{Model: "claude-sonnet-4-6[1m]"}
	name, tokens := m.ParsedModel()
	if name != "claude-sonnet-4-6" || tokens != 1_000_000 {
		t.Errorf("got %q %d", name, tokens)
	}
}

func TestParsedModel_PlainInt(t *testing.T) {
	m := &MergedSettings{Model: "mymodel[4096]"}
	name, tokens := m.ParsedModel()
	if name != "mymodel" || tokens != 4096 {
		t.Errorf("got %q %d", name, tokens)
	}
}

func TestParsedModel_Empty(t *testing.T) {
	m := &MergedSettings{}
	name, tokens := m.ParsedModel()
	if name != "" || tokens != 0 {
		t.Errorf("got %q %d", name, tokens)
	}
}

func TestAPIKey_AuthToken(t *testing.T) {
	m := &MergedSettings{Env: map[string]string{"ANTHROPIC_AUTH_TOKEN": "tok1"}}
	if m.APIKey() != "tok1" {
		t.Errorf("got %q", m.APIKey())
	}
}

func TestAPIKey_APIKeyFallback(t *testing.T) {
	m := &MergedSettings{Env: map[string]string{"ANTHROPIC_API_KEY": "tok2"}}
	if m.APIKey() != "tok2" {
		t.Errorf("got %q", m.APIKey())
	}
}

func TestAPIKey_Empty(t *testing.T) {
	m := &MergedSettings{Env: map[string]string{}}
	if m.APIKey() != "" {
		t.Errorf("expected empty, got %q", m.APIKey())
	}
}

func TestAPIBaseURL(t *testing.T) {
	m := &MergedSettings{Env: map[string]string{"ANTHROPIC_BASE_URL": "https://proxy.example.com"}}
	if m.APIBaseURL() != "https://proxy.example.com" {
		t.Errorf("got %q", m.APIBaseURL())
	}
}

func TestCustomHeaders_Parsing(t *testing.T) {
	m := &MergedSettings{Env: map[string]string{
		"ANTHROPIC_CUSTOM_HEADERS": "X-Foo:bar,X-Baz:qux",
	}}
	h := m.CustomHeaders()
	if h["X-Foo"] != "bar" || h["X-Baz"] != "qux" {
		t.Errorf("headers: %v", h)
	}
}

func TestCustomHeaders_Empty(t *testing.T) {
	m := &MergedSettings{Env: map[string]string{}}
	if m.CustomHeaders() != nil {
		t.Error("expected nil headers")
	}
}

func TestLoadAPIKey_Explicit(t *testing.T) {
	key := LoadAPIKey("explicit-key")
	if key != "explicit-key" {
		t.Errorf("got %q", key)
	}
}

func TestLoadAPIKey_EnvAuthToken(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "env-tok")
	t.Setenv("ANTHROPIC_API_KEY", "")
	key := LoadAPIKey("")
	if key != "env-tok" {
		t.Errorf("got %q", key)
	}
}

func TestLoadAPIKey_EnvAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "api-tok")
	key := LoadAPIKey("")
	if key != "api-tok" {
		t.Errorf("got %q", key)
	}
}

func TestLoadAPIKey_CredentialsFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	cred := `{"apiKey":"cred-tok"}`
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(cred), 0600); err != nil {
		t.Fatal(err)
	}
	key := LoadAPIKey("")
	if key != "cred-tok" {
		t.Errorf("got %q", key)
	}
}

func TestLoadAPIKey_Empty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	key := LoadAPIKey("")
	if key != "" {
		t.Errorf("expected empty, got %q", key)
	}
}

func TestMergeSettings_MCPServers(t *testing.T) {
	layers := []SettingsJson{
		{MCPServers: map[string]MCPServerConfig{
			"fs": {Command: "npx", Args: []string{"-y", "server-fs"}},
		}},
		{MCPServers: map[string]MCPServerConfig{
			"git": {Command: "npx", Args: []string{"-y", "server-git"}},
			"fs":  {Command: "python3", Args: []string{"override.py"}}, // override
		}},
	}
	m := mergeSettings(layers)
	if len(m.MCPServers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(m.MCPServers))
	}
	if m.MCPServers["fs"].Command != "python3" {
		t.Errorf("fs should be overridden to python3, got %q", m.MCPServers["fs"].Command)
	}
	if _, ok := m.MCPServers["git"]; !ok {
		t.Error("missing 'git' server")
	}
}

func TestParseTokenSuffix_InvalidChar(t *testing.T) {
	// non-numeric after stripping suffix → 0
	got := parseTokenSuffix("abc")
	if got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}
