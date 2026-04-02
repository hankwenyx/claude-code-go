// Package tools defines the Tool interface and registry
package tools

import (
	"context"
	"encoding/json"
)

const DefaultMaxResultSize = 50_000 // characters

// PermissionDecision is the result of a permission check
type PermissionDecision struct {
	Behavior string // "allow" | "deny" | "ask"
	Reason   string
}

// PermissionRules holds allow/deny/ask rules
type PermissionRules struct {
	Allow []string
	Deny  []string
	Ask   []string
}

// ToolResult is the output from a tool execution
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// Tool is the interface all tools must implement
type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Call(ctx context.Context, input json.RawMessage) (ToolResult, error)
	CheckPermissions(input json.RawMessage, mode string, rules PermissionRules) PermissionDecision
	IsReadOnly() bool
}
