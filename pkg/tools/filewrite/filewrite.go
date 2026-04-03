// Package filewrite implements the FileWrite tool
package filewrite

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
	"github.com/hankwenyx/claude-code-go/pkg/tools/fileread"
)

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_path": {
      "type": "string",
      "description": "The absolute path to the file to write"
    },
    "content": {
      "type": "string",
      "description": "The content to write to the file"
    }
  },
  "required": ["file_path", "content"]
}`)

// Tool implements the FileWrite tool
type Tool struct {
	State *fileread.StateStore
}

// New creates a new FileWrite tool
func New(state *fileread.StateStore) *Tool {
	return &Tool{State: state}
}

func (t *Tool) Name() string                 { return "Write" }
func (t *Tool) IsReadOnly() bool             { return false }
func (t *Tool) InputSchema() json.RawMessage { return inputSchema }
func (t *Tool) Description() string {
	return `Write content to a file, creating it or overwriting it. Creates parent directories as needed.`
}

type input struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

func (t *Tool) Call(ctx context.Context, rawInput json.RawMessage) (tools.ToolResult, error) {
	var in input
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return tools.ToolResult{IsError: true, Content: "invalid input: " + err.Error()}, nil
	}

	if in.FilePath == "" {
		return tools.ToolResult{IsError: true, Content: "file_path is required"}, nil
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(in.FilePath), 0755); err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("cannot create directories: %v", err)}, nil
	}

	// Preserve existing file permissions; new files get 0666 (umask applied by OS)
	perm := os.FileMode(0666)
	if si, err := os.Stat(in.FilePath); err == nil {
		perm = si.Mode().Perm()
	}

	if err := os.WriteFile(in.FilePath, []byte(in.Content), perm); err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("cannot write file: %v", err)}, nil
	}

	// Update state
	if t.State != nil {
		if si, err := os.Stat(in.FilePath); err == nil {
			t.State.Set(in.FilePath, fileread.FileState{MTime: si.ModTime()})
		}
	}

	return tools.ToolResult{Content: "wrote " + in.FilePath}, nil
}

func (t *Tool) CheckPermissions(rawInput json.RawMessage, mode string, rules tools.PermissionRules) tools.PermissionDecision {
	var in input
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return tools.PermissionDecision{Behavior: "deny", Reason: "invalid input"}
	}

	cfgRules := config.PermissionRules{Allow: rules.Allow, Deny: rules.Deny, Ask: rules.Ask}
	for _, rule := range cfgRules.Deny {
		if config.MatchRule(rule, t.Name(), in.FilePath) {
			return tools.PermissionDecision{Behavior: "deny", Reason: "matched deny rule: " + rule}
		}
	}
	for _, rule := range cfgRules.Ask {
		if config.MatchRule(rule, t.Name(), in.FilePath) {
			return tools.PermissionDecision{Behavior: "ask", Reason: "matched ask rule: " + rule}
		}
	}
	for _, rule := range cfgRules.Allow {
		if config.MatchRule(rule, t.Name(), in.FilePath) {
			return tools.PermissionDecision{Behavior: "allow", Reason: "matched allow rule: " + rule}
		}
	}
	return tools.PermissionDecision{Behavior: "ask", Reason: "no matching allow rule"}
}
