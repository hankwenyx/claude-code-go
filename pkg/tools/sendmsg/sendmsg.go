// Package sendmsg implements the SendUserMessage tool used in brief/chat view mode.
// When registered, the model uses this tool to display messages to the user
// instead of streaming raw text — enabling a cleaner chat-style interface.
package sendmsg

import (
	"context"
	"encoding/json"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

const schema = `{
  "type": "object",
  "properties": {
    "message": {
      "type": "string",
      "description": "The message to display to the user."
    }
  },
  "required": ["message"]
}`

// Tool is the SendUserMessage tool implementation.
type Tool struct{}

// New returns a new SendUserMessage tool.
func New() *Tool { return &Tool{} }

func (t *Tool) Name() string        { return "SendUserMessage" }
func (t *Tool) IsReadOnly() bool    { return true }
func (t *Tool) Description() string { return "Send a message to the user in the chat view." }
func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(schema)
}

type input struct {
	Message string `json:"message"`
}

// Call returns the message text as the tool result.
// The TUI intercepts EventToolUse for this tool and renders the message directly.
func (t *Tool) Call(_ context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return tools.ToolResult{IsError: true, Content: "invalid input: " + err.Error()}, nil
	}
	return tools.ToolResult{Content: in.Message}, nil
}

// CheckPermissions always allows SendUserMessage — it only sends text to the user.
func (t *Tool) CheckPermissions(_ json.RawMessage, _ string, _ tools.PermissionRules) tools.PermissionDecision {
	return tools.PermissionDecision{Behavior: "allow", Reason: "read-only display tool"}
}
