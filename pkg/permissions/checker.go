// Package permissions provides the permission checking engine
package permissions

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/hankwenyx/claude-code-go/pkg/config"
)

// Behavior constants
const (
	BehaviorAllow = "allow"
	BehaviorDeny  = "deny"
	BehaviorAsk   = "ask"
)

// Decision is the result of a permission check
type Decision struct {
	Behavior string // "allow" | "deny" | "ask"
	Reason   string
}

// Mode represents the permission mode
type Mode string

const (
	ModeDefault           Mode = "default"
	ModeBypassPermissions Mode = "bypassPermissions"
	ModeDontAsk           Mode = "dontAsk"
	ModeAuto              Mode = "auto"
)

// Checker performs permission checks for tool calls
type Checker struct {
	Mode           Mode
	Rules          config.PermissionRules
	AdditlDirs     []string // additionalDirectories
	CWD            string
	NonInteractive bool // headless: ask → deny

	// AskFunc is called when interactive permission is needed.
	// Returns true=allow, false=deny. nil = fall back to BehaviorAsk.
	AskFunc func(toolName, arg, reason string) bool
}

// Tool is the minimal interface needed by the checker
type Tool interface {
	Name() string
	IsReadOnly() bool
}

// Check performs a permission check for the given tool and input
func (c *Checker) Check(tool Tool, inputJSON json.RawMessage) Decision {
	if c.Mode == ModeBypassPermissions {
		return Decision{Behavior: BehaviorAllow, Reason: "bypass mode"}
	}

	name := tool.Name()
	arg := extractArg(name, inputJSON)

	// Check deny rules first
	for _, rule := range c.Rules.Deny {
		if config.MatchRule(rule, name, arg) {
			return Decision{Behavior: BehaviorDeny, Reason: "matched deny rule: " + rule}
		}
	}

	// Check ask rules
	for _, rule := range c.Rules.Ask {
		if config.MatchRule(rule, name, arg) {
			return c.askOrDeny(name, arg, "matched ask rule: "+rule)
		}
	}

	// File reads: allow if in cwd or additionalDirs
	if tool.IsReadOnly() && isFileTool(name) {
		if c.isAllowedPath(arg) {
			return Decision{Behavior: BehaviorAllow, Reason: "path within allowed directory"}
		}
	}

	// File writes: safety checks
	if !tool.IsReadOnly() && isFileTool(name) {
		if isDangerousPath(arg) {
			return c.askOrDeny(name, arg, "dangerous path: "+arg)
		}
		if c.Mode == ModeAuto || c.Mode == ModeDontAsk {
			if c.isAllowedPath(arg) {
				return Decision{Behavior: BehaviorAllow, Reason: "auto mode: path within allowed directory"}
			}
		}
	}

	// Check allow rules
	for _, rule := range c.Rules.Allow {
		if config.MatchRule(rule, name, arg) {
			return Decision{Behavior: BehaviorAllow, Reason: "matched allow rule: " + rule}
		}
	}

	// Default: ask (headless → deny)
	return c.askOrDeny(name, arg, "no matching allow rule")
}

func (c *Checker) askOrDeny(toolName, arg, reason string) Decision {
	if c.NonInteractive {
		return Decision{Behavior: BehaviorDeny, Reason: reason + " (non-interactive: ask→deny)"}
	}
	if c.AskFunc != nil {
		if c.AskFunc(toolName, arg, reason) {
			return Decision{Behavior: BehaviorAllow, Reason: "user approved"}
		}
		return Decision{Behavior: BehaviorDeny, Reason: "user denied"}
	}
	return Decision{Behavior: BehaviorAsk, Reason: reason}
}

func (c *Checker) isAllowedPath(path string) bool {
	if path == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Check CWD
	if c.CWD != "" {
		cwdAbs, _ := filepath.Abs(c.CWD)
		if strings.HasPrefix(abs, cwdAbs+string(filepath.Separator)) || abs == cwdAbs {
			return true
		}
	}

	// Check additional directories
	for _, dir := range c.AdditlDirs {
		dirAbs, _ := filepath.Abs(dir)
		if strings.HasPrefix(abs, dirAbs+string(filepath.Separator)) || abs == dirAbs {
			return true
		}
	}
	return false
}

// isFileTool returns true for file-related tools
func isFileTool(name string) bool {
	switch strings.ToLower(name) {
	case "fileread", "read", "fileedit", "edit", "filewrite", "write", "glob", "grep":
		return true
	}
	return false
}

// dangerousDirs and dangerousFiles match the JS DANGEROUS_DIRECTORIES / DANGEROUS_FILES
var dangerousDirs = []string{".git", ".vscode", ".idea", ".claude"}
var dangerousFiles = []string{".gitconfig", ".bashrc", ".zshrc", ".mcp.json", ".claude.json"}

func isDangerousPath(path string) bool {
	base := filepath.Base(path)
	for _, f := range dangerousFiles {
		if base == f {
			return true
		}
	}
	// Check if any component is a dangerous dir
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts {
		for _, d := range dangerousDirs {
			if p == d {
				return true
			}
		}
	}
	return false
}

// extractArg extracts the primary argument from tool input JSON
// For Bash: "command" field; for file tools: "path" or "file_path"
func extractArg(toolName string, inputJSON json.RawMessage) string {
	if len(inputJSON) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(inputJSON, &m); err != nil {
		return ""
	}

	var tryKeys []string
	switch strings.ToLower(toolName) {
	case "bash":
		tryKeys = []string{"command"}
	default:
		tryKeys = []string{"path", "file_path", "command", "pattern"}
	}

	for _, k := range tryKeys {
		if v, ok := m[k]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				return s
			}
		}
	}
	return ""
}
