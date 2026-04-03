// Package agent provides the core agent loop for Claude Code
package agent

import (
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/api"
)

// EventType represents the type of an agent event
type EventType string

const (
	// EventText is emitted when the model outputs text (streaming)
	EventText EventType = "text"
	// EventToolUse is emitted when a tool call starts
	EventToolUse EventType = "tool_use"
	// EventToolResult is emitted when a tool execution completes
	EventToolResult EventType = "tool_result"
	// EventMessage is emitted when a complete assistant message is ready
	EventMessage EventType = "message"
	// EventError is emitted when an unrecoverable error occurs
	EventError EventType = "error"
	// EventRetry is emitted before each retry attempt so the UI can show a wait indicator
	EventRetry EventType = "retry"
	// EventCompact is emitted when the conversation history is auto-compacted
	// to stay within the CompactThreshold token budget.
	EventCompact EventType = "compact"
)

// AgentEvent represents an event from the agent loop
type AgentEvent struct {
	Type       EventType
	Text       string           // EventText: text content
	ToolCall   *ToolCall        // EventToolUse
	ToolResult *ToolResult      // EventToolResult
	Message    *api.Message     // EventMessage
	Messages   []api.APIMessage // EventMessage: full updated conversation history
	Usage      *api.Usage       // EventMessage: token usage for this turn
	Error      error            // EventError
	// EventRetry fields
	RetryErr error
	RetryIn  time.Duration
	Attempt  int
}

// ToolCall represents a tool call from the model
type ToolCall struct {
	ID    string
	Name  string
	Input []byte // JSON bytes
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}
