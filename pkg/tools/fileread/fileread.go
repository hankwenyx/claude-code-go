// Package fileread implements the FileRead tool
package fileread

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_path": {
      "type": "string",
      "description": "The absolute path to the file to read"
    },
    "offset": {
      "type": "integer",
      "description": "Line number to start reading from (1-indexed)"
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of lines to read"
    }
  },
  "required": ["file_path"]
}`)

// FileState tracks file read state for edit validation
type FileState struct {
	MTime time.Time
}

// StateStore stores file read states (used by FileEdit to validate)
type StateStore struct {
	mu    sync.Mutex
	files map[string]FileState
}

// NewStateStore creates a new StateStore
func NewStateStore() *StateStore {
	return &StateStore{files: make(map[string]FileState)}
}

// Set records a file's state
func (s *StateStore) Set(path string, state FileState) {
	s.mu.Lock()
	s.files[path] = state
	s.mu.Unlock()
}

// Get retrieves a file's state
func (s *StateStore) Get(path string) (FileState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.files[path]
	return st, ok
}

// Tool implements the FileRead tool
type Tool struct {
	State *StateStore
}

// New creates a new FileRead tool
func New(state *StateStore) *Tool {
	return &Tool{State: state}
}

func (t *Tool) Name() string             { return "Read" }
func (t *Tool) IsReadOnly() bool         { return true }
func (t *Tool) InputSchema() json.RawMessage { return inputSchema }
func (t *Tool) Description() string {
	return `Read a file from the filesystem. Returns file content with line numbers (cat -n format). Supports offset and limit for pagination.`
}

type input struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

func (t *Tool) Call(ctx context.Context, rawInput json.RawMessage) (tools.ToolResult, error) {
	var in input
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return tools.ToolResult{IsError: true, Content: "invalid input: " + err.Error()}, nil
	}

	if in.FilePath == "" {
		return tools.ToolResult{IsError: true, Content: "file_path is required"}, nil
	}

	info, err := os.Stat(in.FilePath)
	if err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("cannot stat file: %v", err)}, nil
	}

	// Track file state for FileEdit validation
	if t.State != nil {
		t.State.Set(in.FilePath, FileState{MTime: info.ModTime()})
	}

	f, err := os.Open(in.FilePath)
	if err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("cannot open file: %v", err)}, nil
	}
	defer f.Close()

	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	lineNum := 0
	written := 0
	limit := in.Limit
	if limit == 0 {
		limit = 2000 // default
	}

	for scanner.Scan() {
		lineNum++
		if in.Offset > 0 && lineNum < in.Offset {
			continue
		}
		if written >= limit {
			break
		}
		sb.WriteString(fmt.Sprintf("%6d\t%s\n", lineNum, scanner.Text()))
		written++
	}

	if err := scanner.Err(); err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("read error: %v", err)}, nil
	}

	return tools.ToolResult{Content: sb.String()}, nil
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
	// Read tools: allow if no rules say otherwise (permissive default for reads)
	return tools.PermissionDecision{Behavior: "allow", Reason: "read-only default allow"}
}
