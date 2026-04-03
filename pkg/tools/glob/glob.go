// Package glob implements the Glob tool
package glob

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

const maxResults = 250

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {
      "type": "string",
      "description": "The glob pattern to match files against (e.g. \"**/*.go\")"
    },
    "path": {
      "type": "string",
      "description": "The directory to search in (default: current directory)"
    }
  },
  "required": ["pattern"]
}`)

// Tool implements the Glob tool
type Tool struct {
	CWD string
}

// New creates a new Glob tool
func New(cwd string) *Tool {
	return &Tool{CWD: cwd}
}

func (t *Tool) Name() string                 { return "Glob" }
func (t *Tool) IsReadOnly() bool             { return true }
func (t *Tool) InputSchema() json.RawMessage { return inputSchema }
func (t *Tool) Description() string {
	return `Find files matching a glob pattern. Sorted by modification time (newest first). Returns up to 250 results.`
}

type input struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type fileEntry struct {
	path  string
	mtime int64
}

func (t *Tool) Call(ctx context.Context, rawInput json.RawMessage) (tools.ToolResult, error) {
	var in input
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return tools.ToolResult{IsError: true, Content: "invalid input: " + err.Error()}, nil
	}

	base := in.Path
	if base == "" {
		base = t.CWD
	}
	if base == "" {
		base = "."
	}

	// doublestar.Glob needs a fs.FS rooted at base
	fsys := os.DirFS(base)
	matches, err := doublestar.Glob(fsys, in.Pattern)
	if err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("glob error: %v", err)}, nil
	}

	var entries []fileEntry
	for _, rel := range matches {
		abs := filepath.Join(base, rel)
		info, statErr := os.Lstat(abs)
		if statErr != nil || info.IsDir() {
			continue
		}
		entries = append(entries, fileEntry{path: abs, mtime: info.ModTime().UnixNano()})
	}

	// Sort by mtime descending (newest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].mtime > entries[j].mtime
	})

	if len(entries) > maxResults {
		entries = entries[:maxResults]
	}

	var result []byte
	for _, e := range entries {
		result = append(result, []byte(e.path+"\n")...)
	}

	if len(result) == 0 {
		return tools.ToolResult{Content: "no files found"}, nil
	}
	return tools.ToolResult{Content: string(result)}, nil
}

func (t *Tool) CheckPermissions(rawInput json.RawMessage, mode string, rules tools.PermissionRules) tools.PermissionDecision {
	cfgRules := config.PermissionRules{Allow: rules.Allow, Deny: rules.Deny, Ask: rules.Ask}
	var in input
	_ = json.Unmarshal(rawInput, &in)

	for _, rule := range cfgRules.Deny {
		if config.MatchRule(rule, t.Name(), in.Pattern) {
			return tools.PermissionDecision{Behavior: "deny", Reason: "matched deny rule: " + rule}
		}
	}
	return tools.PermissionDecision{Behavior: "allow", Reason: "read-only default allow"}
}
