// Package config provides configuration loading compatible with the original Claude Code
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PermissionsSettings holds permission allow/deny/ask rules
type PermissionsSettings struct {
	Allow          []string `json:"allow,omitempty"`
	Deny           []string `json:"deny,omitempty"`
	Ask            []string `json:"ask,omitempty"`
	DefaultMode    string   `json:"defaultMode,omitempty"` // "default"|"bypassPermissions"|"plan"|"auto"|"dontAsk"
	AdditionalDirs []string `json:"additionalDirectories,omitempty"`
}

// MCPServerConfig describes a single MCP server entry in settings.json.
// Either Command (stdio) or URL (SSE) must be set.
type MCPServerConfig struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

// SettingsJson maps to a single settings.json file
// HookDef is a single hook command entry.
type HookDef struct {
	Type      string `json:"type"`                // "command"
	Command   string `json:"command"`             // shell command
	TimeoutMs int    `json:"timeoutMs,omitempty"` // 0 = 60s default
}

// HookGroup pairs a tool-name matcher with a list of hook commands.
// An empty Matcher matches all tools.
type HookGroup struct {
	Matcher string    `json:"matcher,omitempty"`
	Hooks   []HookDef `json:"hooks"`
}

// HooksConfig mirrors the "hooks" key in settings.json.
type HooksConfig struct {
	PreToolUse   []HookGroup `json:"PreToolUse,omitempty"`
	PostToolUse  []HookGroup `json:"PostToolUse,omitempty"`
	Notification []HookGroup `json:"Notification,omitempty"`
	Stop         []HookGroup `json:"Stop,omitempty"`
}

type SettingsJson struct {
	Model             string                     `json:"model,omitempty"`
	Permissions       PermissionsSettings        `json:"permissions,omitempty"`
	Env               map[string]string          `json:"env,omitempty"`
	RespectGitignore  *bool                      `json:"respectGitignore,omitempty"`
	AlwaysThinking    *bool                      `json:"alwaysThinkingEnabled,omitempty"`
	Language          string                     `json:"language,omitempty"`
	OutputStyle       string                     `json:"outputStyle,omitempty"`
	CleanupPeriodDays *int                       `json:"cleanupPeriodDays,omitempty"`
	DefaultShell      string                     `json:"defaultShell,omitempty"` // "bash"|"powershell"
	MCPServers        map[string]MCPServerConfig `json:"mcpServers,omitempty"`
	Hooks             HooksConfig                `json:"hooks,omitempty"`
}

// MergedSettings is the final merged configuration
type MergedSettings struct {
	Model            string
	Permissions      PermissionsSettings
	Env              map[string]string
	RespectGitignore bool
	AlwaysThinking   bool
	Language         string
	DefaultShell     string
	MCPServers       map[string]MCPServerConfig
	Hooks            HooksConfig
}

// APIKey returns the best available API key from merged env, checking
// ANTHROPIC_AUTH_TOKEN and ANTHROPIC_API_KEY in that order.
func (m *MergedSettings) APIKey() string {
	if v := m.Env["ANTHROPIC_AUTH_TOKEN"]; v != "" {
		return v
	}
	return m.Env["ANTHROPIC_API_KEY"]
}

// APIBaseURL returns the base URL override from merged env, if set.
func (m *MergedSettings) APIBaseURL() string {
	return m.Env["ANTHROPIC_BASE_URL"]
}

// ParsedModel returns the model name with any inline parameter suffix stripped.
// For example "opus[1m]" → model="opus", maxTokens=1000000.
// If no suffix is present, maxTokens is 0 (caller uses default).
func (m *MergedSettings) ParsedModel() (modelName string, maxTokens int) {
	raw := m.Model
	if raw == "" {
		return "", 0
	}
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return raw, 0
	}
	modelName = raw[:start]
	param := raw[start+1 : end]
	// Parse suffix like "1m" → 1_000_000, "200k" → 200_000, plain int → int
	maxTokens = parseTokenSuffix(param)
	return modelName, maxTokens
}

func parseTokenSuffix(s string) int {
	if s == "" {
		return 0
	}
	mult := 1
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, "m") {
		mult = 1_000_000
		s = s[:len(s)-1]
	} else if strings.HasSuffix(lower, "k") {
		mult = 1_000
		s = s[:len(s)-1]
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n * mult
}

// CustomHeaders returns additional HTTP headers from merged env (ANTHROPIC_CUSTOM_HEADERS),
// parsed as comma-separated "key:value" pairs.
func (m *MergedSettings) CustomHeaders() map[string]string {
	raw := m.Env["ANTHROPIC_CUSTOM_HEADERS"]
	if raw == "" {
		return nil
	}
	result := make(map[string]string)
	for _, pair := range splitComma(raw) {
		if idx := indexByte(pair, ':'); idx > 0 {
			result[pair[:idx]] = pair[idx+1:]
		}
	}
	return result
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// LoadSettings loads and merges all settings files with priority:
// Plugin < User < Project < Local < Policy(Managed)
func LoadSettings(cwd string) (*MergedSettings, error) {
	var layers []SettingsJson

	// 1. User settings: ~/.claude/settings.json
	userDir := userConfigDir()
	if s, err := loadSettingsFile(filepath.Join(userDir, "settings.json")); err == nil {
		layers = append(layers, s)
	}

	// 2. Project settings: <cwd>/.claude/settings.json
	if s, err := loadSettingsFile(filepath.Join(cwd, ".claude", "settings.json")); err == nil {
		layers = append(layers, s)
	}

	// 3. Local settings: <cwd>/.claude/settings.local.json
	if s, err := loadSettingsFile(filepath.Join(cwd, ".claude", "settings.local.json")); err == nil {
		layers = append(layers, s)
	}

	// 4. Managed/Policy settings
	for _, p := range managedSettingsPaths() {
		if s, err := loadSettingsFile(p); err == nil {
			layers = append(layers, s)
		}
	}

	return mergeSettings(layers), nil
}

func loadSettingsFile(path string) (SettingsJson, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SettingsJson{}, err
	}
	var s SettingsJson
	if err := json.Unmarshal(data, &s); err != nil {
		return SettingsJson{}, err
	}
	return s, nil
}

func mergeSettings(layers []SettingsJson) *MergedSettings {
	m := &MergedSettings{
		RespectGitignore: true, // default
		Env:              make(map[string]string),
		MCPServers:       make(map[string]MCPServerConfig),
	}

	for _, s := range layers {
		if s.Model != "" {
			m.Model = s.Model
		}
		if s.Language != "" {
			m.Language = s.Language
		}
		if s.DefaultShell != "" {
			m.DefaultShell = s.DefaultShell
		}
		if s.RespectGitignore != nil {
			m.RespectGitignore = *s.RespectGitignore
		}
		if s.AlwaysThinking != nil {
			m.AlwaysThinking = *s.AlwaysThinking
		}

		// Merge env (later overrides earlier)
		for k, v := range s.Env {
			m.Env[k] = v
		}

		// Merge mcpServers (later overrides earlier by name)
		for k, v := range s.MCPServers {
			m.MCPServers[k] = v
		}

		// Merge permissions - arrays are appended and deduplicated
		m.Permissions.Allow = mergeStringSlice(m.Permissions.Allow, s.Permissions.Allow)
		m.Permissions.Deny = mergeStringSlice(m.Permissions.Deny, s.Permissions.Deny)
		m.Permissions.Ask = mergeStringSlice(m.Permissions.Ask, s.Permissions.Ask)
		m.Permissions.AdditionalDirs = mergeStringSlice(m.Permissions.AdditionalDirs, s.Permissions.AdditionalDirs)

		if s.Permissions.DefaultMode != "" {
			m.Permissions.DefaultMode = s.Permissions.DefaultMode
		}

		// Merge hooks — later layers append their groups
		m.Hooks.PreToolUse = append(m.Hooks.PreToolUse, s.Hooks.PreToolUse...)
		m.Hooks.PostToolUse = append(m.Hooks.PostToolUse, s.Hooks.PostToolUse...)
		m.Hooks.Notification = append(m.Hooks.Notification, s.Hooks.Notification...)
		m.Hooks.Stop = append(m.Hooks.Stop, s.Hooks.Stop...)
	}

	return m
}

func mergeStringSlice(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	result := make([]string, 0, len(a)+len(b))
	for _, v := range a {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	for _, v := range b {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// credentials holds the structure of ~/.claude/.credentials.json
type credentials struct {
	APIKey string `json:"apiKey"`
}

// LoadAPIKey returns the best available Anthropic API key, checked in order:
//  1. Explicit key argument (non-empty)
//  2. ANTHROPIC_AUTH_TOKEN environment variable
//  3. ANTHROPIC_API_KEY environment variable
//  4. ~/.claude/.credentials.json
func LoadAPIKey(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("ANTHROPIC_AUTH_TOKEN"); v != "" {
		return v
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		return v
	}
	// Try credentials file
	userDir := userConfigDir()
	data, err := os.ReadFile(filepath.Join(userDir, ".credentials.json"))
	if err == nil {
		var cred credentials
		if json.Unmarshal(data, &cred) == nil && cred.APIKey != "" {
			return cred.APIKey
		}
	}
	return ""
}

// userConfigDir returns the user config directory, respecting CLAUDE_CONFIG_DIR env var
func userConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// managedSettingsPaths returns platform-specific managed settings paths
func managedSettingsPaths() []string {
	switch runtime.GOOS {
	case "linux":
		return []string{"/etc/claude-code/managed-settings.json"}
	case "darwin":
		return []string{"/Library/Application Support/ClaudeCode/managed-settings.json"}
	case "windows":
		return []string{`C:\Program Files\ClaudeCode\managed-settings.json`}
	}
	return nil
}
