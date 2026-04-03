// Package task provides the Task tool, which runs a sub-agent synchronously
// or asynchronously to complete a delegated prompt.
package task

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

// SubAgentRunner is a function that executes a sub-agent and returns the final text.
// Using a function type avoids an import cycle between tools/task and pkg/agent.
type SubAgentRunner func(ctx context.Context, prompt string) (string, error)

// Tool is the synchronous Task tool.
// It delegates a prompt to a sub-agent and blocks until completion.
type Tool struct {
	runner  SubAgentRunner
	manager *Manager // nil means sync-only
}

// New creates a synchronous Task tool.
func New(runner SubAgentRunner) *Tool {
	return &Tool{runner: runner}
}

// NewWithManager creates a Task tool that also supports async dispatch via manager.
func NewWithManager(runner SubAgentRunner, manager *Manager) *Tool {
	return &Tool{runner: runner, manager: manager}
}

func (t *Tool) Name() string     { return "Task" }
func (t *Tool) IsReadOnly() bool { return false }

func (t *Tool) Description() string {
	return "Run a sub-agent to complete a self-contained task. " +
		"The sub-agent has access to all tools and runs to completion before returning. " +
		"Use this to delegate complex, multi-step work."
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"description": {
				"type": "string",
				"description": "Short human-readable description of what this task does"
			},
			"prompt": {
				"type": "string",
				"description": "The full prompt for the sub-agent"
			},
			"async": {
				"type": "boolean",
				"description": "If true, start the task in the background and return a task_id immediately"
			}
		},
		"required": ["description", "prompt"]
	}`)
}

type taskInput struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Async       bool   `json:"async"`
}

// Call runs the sub-agent. If async=true and a manager is configured, it dispatches
// the task to the background and returns a task_id. Otherwise it runs synchronously.
func (t *Tool) Call(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	var in taskInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return tools.ToolResult{IsError: true}, fmt.Errorf("Task: invalid input: %w", err)
	}
	if in.Prompt == "" {
		return tools.ToolResult{Content: "", IsError: true}, fmt.Errorf("Task: prompt is required")
	}

	// Async path
	if in.Async && t.manager != nil {
		taskID := t.manager.Dispatch(ctx, in.Description, in.Prompt, t.runner)
		return tools.ToolResult{
			Content: fmt.Sprintf(`{"task_id":%q,"status":"running","description":%q}`, taskID, in.Description),
		}, nil
	}

	// Synchronous path
	result, err := t.runner(ctx, in.Prompt)
	if err != nil {
		return tools.ToolResult{
			Content: fmt.Sprintf("Task failed: %v", err),
			IsError: true,
		}, nil // return nil error so the model sees the error text
	}
	return tools.ToolResult{Content: result}, nil
}

// CheckPermissions always asks — launching sub-agents is a significant action.
func (t *Tool) CheckPermissions(_ json.RawMessage, mode string, rules tools.PermissionRules) tools.PermissionDecision {
	for _, r := range rules.Deny {
		if r == "Task" || r == "Task(*)" {
			return tools.PermissionDecision{Behavior: "deny", Reason: "matched deny rule: " + r}
		}
	}
	for _, r := range rules.Allow {
		if r == "Task" || r == "Task(*)" {
			return tools.PermissionDecision{Behavior: "allow", Reason: "matched allow rule: " + r}
		}
	}
	if mode == "bypassPermissions" {
		return tools.PermissionDecision{Behavior: "allow", Reason: "bypass mode"}
	}
	return tools.PermissionDecision{Behavior: "ask", Reason: "Task tool requires permission"}
}
