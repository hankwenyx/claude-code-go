// Package bash implements the Bash tool
package bash

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

const maxOutputChars = 30_000

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "description": "The bash command to execute"
    },
    "description": {
      "type": "string",
      "description": "A short description of what this command does"
    }
  },
  "required": ["command"]
}`)

// Tool implements the Bash tool
type Tool struct {
	Shell string // default "bash"
}

// New creates a new Bash tool
func New() *Tool {
	return &Tool{Shell: "bash"}
}

func (t *Tool) Name() string        { return "Bash" }
func (t *Tool) IsReadOnly() bool    { return false }
func (t *Tool) InputSchema() json.RawMessage { return inputSchema }
func (t *Tool) Description() string {
	return `Execute a bash command and return its output. Use this for running shell commands, scripts, and system operations.`
}

type input struct {
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
}

func (t *Tool) Call(ctx context.Context, rawInput json.RawMessage) (tools.ToolResult, error) {
	var in input
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return tools.ToolResult{IsError: true, Content: "invalid input: " + err.Error()}, nil
	}

	shell := t.Shell
	if shell == "" {
		shell = "bash"
	}

	cmd := exec.CommandContext(ctx, shell, "-c", in.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	out := stdout.String()
	errOut := stderr.String()

	var combined strings.Builder
	combined.WriteString(out)
	if errOut != "" {
		if out != "" {
			combined.WriteString("\n")
		}
		combined.WriteString(errOut)
	}

	result := combined.String()
	isError := err != nil

	if len(result) > maxOutputChars {
		result = result[:maxOutputChars] + "\n... (output truncated)"
	}

	if result == "" && isError {
		result = "Command failed with no output: " + err.Error()
	}

	return tools.ToolResult{Content: result, IsError: isError}, nil
}

func (t *Tool) CheckPermissions(rawInput json.RawMessage, mode string, rules tools.PermissionRules) tools.PermissionDecision {
	var in input
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return tools.PermissionDecision{Behavior: "deny", Reason: "invalid input"}
	}

	cfgRules := config.PermissionRules{
		Allow: rules.Allow,
		Deny:  rules.Deny,
		Ask:   rules.Ask,
	}

	// Check deny first
	for _, rule := range cfgRules.Deny {
		if config.MatchRule(rule, t.Name(), in.Command) {
			return tools.PermissionDecision{Behavior: "deny", Reason: "matched deny rule: " + rule}
		}
	}
	// Check ask
	for _, rule := range cfgRules.Ask {
		if config.MatchRule(rule, t.Name(), in.Command) {
			return tools.PermissionDecision{Behavior: "ask", Reason: "matched ask rule: " + rule}
		}
	}
	// Check allow
	for _, rule := range cfgRules.Allow {
		if config.MatchRule(rule, t.Name(), in.Command) {
			return tools.PermissionDecision{Behavior: "allow", Reason: "matched allow rule: " + rule}
		}
	}
	return tools.PermissionDecision{Behavior: "ask", Reason: "no matching allow rule"}
}
