// Package fileedit implements the FileEdit tool
package fileedit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
	"github.com/hankwenyx/claude-code-go/pkg/tools/fileread"
)

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_path": {
      "type": "string",
      "description": "The absolute path to the file to modify"
    },
    "old_string": {
      "type": "string",
      "description": "The text to replace"
    },
    "new_string": {
      "type": "string",
      "description": "The replacement text"
    },
    "replace_all": {
      "type": "boolean",
      "description": "Replace all occurrences (default: false)"
    }
  },
  "required": ["file_path", "old_string", "new_string"]
}`)

// Tool implements the FileEdit tool
type Tool struct {
	State *fileread.StateStore
}

// New creates a new FileEdit tool
func New(state *fileread.StateStore) *Tool {
	return &Tool{State: state}
}

func (t *Tool) Name() string                 { return "Edit" }
func (t *Tool) IsReadOnly() bool             { return false }
func (t *Tool) InputSchema() json.RawMessage { return inputSchema }
func (t *Tool) Description() string {
	return `Edit a file by replacing text. The file must have been read first. old_string must uniquely match. Use replace_all to replace every occurrence.`
}

type input struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (t *Tool) Call(ctx context.Context, rawInput json.RawMessage) (tools.ToolResult, error) {
	var in input
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return tools.ToolResult{IsError: true, Content: "invalid input: " + err.Error()}, nil
	}

	// Error code 1: no change
	if in.OldString == in.NewString {
		return tools.ToolResult{IsError: true, Content: "old_string and new_string are identical — no change needed"}, nil
	}

	// Error code 6: file not read first
	if t.State != nil {
		if _, ok := t.State.Get(in.FilePath); !ok {
			return tools.ToolResult{IsError: true, Content: "file has not been read — use the Read tool first"}, nil
		}
	}

	// Error code 4: file doesn't exist (and old_string != "")
	info, err := os.Stat(in.FilePath)
	if err != nil {
		if in.OldString != "" {
			return tools.ToolResult{IsError: true, Content: fmt.Sprintf("file does not exist: %s", in.FilePath)}, nil
		}
		// old_string == "" → create new file
		if err := os.WriteFile(in.FilePath, []byte(in.NewString), 0644); err != nil {
			return tools.ToolResult{IsError: true, Content: fmt.Sprintf("cannot create file: %v", err)}, nil
		}
		if t.State != nil {
			si, _ := os.Stat(in.FilePath)
			t.State.Set(in.FilePath, fileread.FileState{MTime: si.ModTime()})
		}
		return tools.ToolResult{Content: "created " + in.FilePath}, nil
	}

	// Error code 3: old_string == "" but file already exists
	if in.OldString == "" {
		return tools.ToolResult{IsError: true, Content: "old_string is empty but file already exists — use a non-empty old_string"}, nil
	}

	// Error code 7: mtime check
	if t.State != nil {
		if st, ok := t.State.Get(in.FilePath); ok {
			if !info.ModTime().Equal(st.MTime) {
				return tools.ToolResult{IsError: true, Content: "file has been modified since last read — re-read it first"}, nil
			}
		}
	}

	data, err := os.ReadFile(in.FilePath)
	if err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("cannot read file: %v", err)}, nil
	}
	content := string(data)

	// Error code 8: old_string not found
	if !strings.Contains(content, in.OldString) {
		return tools.ToolResult{IsError: true, Content: "old_string not found in file"}, nil
	}

	// Error code 9: multiple matches without replace_all
	if !in.ReplaceAll {
		count := strings.Count(content, in.OldString)
		if count > 1 {
			return tools.ToolResult{IsError: true, Content: fmt.Sprintf("old_string appears %d times — use replace_all:true or provide more context", count)}, nil
		}
	}

	var newContent string
	if in.ReplaceAll {
		newContent = strings.ReplaceAll(content, in.OldString, in.NewString)
	} else {
		newContent = strings.Replace(content, in.OldString, in.NewString, 1)
	}

	if err := os.WriteFile(in.FilePath, []byte(newContent), info.Mode()); err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("cannot write file: %v", err)}, nil
	}

	// Update state
	if t.State != nil {
		if si, err := os.Stat(in.FilePath); err == nil {
			t.State.Set(in.FilePath, fileread.FileState{MTime: si.ModTime()})
		} else {
			t.State.Set(in.FilePath, fileread.FileState{MTime: time.Now()})
		}
	}

	return tools.ToolResult{Content: "edited " + in.FilePath}, nil
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
