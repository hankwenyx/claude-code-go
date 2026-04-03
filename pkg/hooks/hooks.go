// Package hooks implements pre/post tool-use and notification hooks.
// Hooks are shell commands configured in settings.json that run at specific
// lifecycle points, compatible with the original Claude Code hook system.
//
// Configuration format in settings.json:
//
//	"hooks": {
//	  "PreToolUse": [
//	    {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo pre"}]}
//	  ],
//	  "PostToolUse": [...],
//	  "Notification": [...],
//	  "Stop": [...]
//	}
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/config"
)

// PreToolInput is the JSON payload sent to a PreToolUse hook command on stdin.
type PreToolInput struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// PreToolOutput is what a PreToolUse hook may return on stdout.
// If the hook exits non-zero OR returns {"action":"block"}, the tool call is blocked.
type PreToolOutput struct {
	// Action may be "block" to prevent the tool call, or "" / "continue" to proceed.
	Action string `json:"action,omitempty"`
	// Reason is surfaced to the model when the tool is blocked.
	Reason string `json:"reason,omitempty"`
	// ModifiedInput, when non-nil, replaces the original tool input.
	ModifiedInput json.RawMessage `json:"modified_input,omitempty"`
}

// PostToolInput is the JSON payload sent to a PostToolUse hook.
type PostToolInput struct {
	ToolName   string          `json:"tool_name"`
	ToolInput  json.RawMessage `json:"tool_input"`
	ToolOutput string          `json:"tool_output"`
	IsError    bool            `json:"is_error"`
}

// PostToolOutput is what a PostToolUse hook may return on stdout.
// ModifiedOutput, if non-empty, replaces the original tool output.
type PostToolOutput struct {
	ModifiedOutput string `json:"modified_output,omitempty"`
}

// NotificationInput is the payload sent to a Notification hook.
type NotificationInput struct {
	Message string `json:"message"`
}

const defaultTimeoutMs = 60_000

// Runner executes hooks for a given event.
type Runner struct {
	cfg config.HooksConfig
	// CWD is the working directory for hook commands; defaults to os.Getwd().
	CWD string
}

// New creates a Runner from a config.HooksConfig.
func New(cfg config.HooksConfig) *Runner {
	return &Runner{cfg: cfg}
}

// matchesGroup returns true if the tool name matches the group's matcher.
// An empty matcher matches all tools.
// A matcher like "Bash(git *)" is matched by stripping the parameter part.
// A matcher ending in "*" is a prefix match.
func matchesGroup(matcher, toolName string) bool {
	if matcher == "" {
		return true
	}
	// Strip parameter portion: "Bash(git *)" → "Bash"
	base := matcher
	if i := strings.IndexByte(matcher, '('); i >= 0 {
		base = matcher[:i]
	}
	if strings.HasSuffix(base, "*") {
		return strings.HasPrefix(toolName, strings.TrimSuffix(base, "*"))
	}
	return base == toolName
}

// RunPreToolUse executes all matching PreToolUse hooks.
// Returns (modifiedInput, blockReason, error).
// modifiedInput is non-nil only when a hook returned a replacement input.
// blockReason is non-empty when the tool call should be blocked.
func (r *Runner) RunPreToolUse(ctx context.Context, toolName string, input json.RawMessage) (json.RawMessage, string, error) {
	payload, _ := json.Marshal(PreToolInput{ToolName: toolName, ToolInput: input})

	modifiedInput := input
	modified := false
	for _, group := range r.cfg.PreToolUse {
		if !matchesGroup(group.Matcher, toolName) {
			continue
		}
		for _, h := range group.Hooks {
			out, blocked, reason, err := runCommand(ctx, h, payload, r.CWD)
			if err != nil {
				return nil, "", fmt.Errorf("PreToolUse hook %q: %w", h.Command, err)
			}
			if blocked {
				if reason == "" {
					reason = "blocked by hook"
				}
				return nil, reason, nil
			}
			if len(out) > 0 {
				var resp PreToolOutput
				if json.Unmarshal(out, &resp) == nil {
					if resp.Action == "block" {
						return nil, resp.Reason, nil
					}
					if len(resp.ModifiedInput) > 0 {
						modifiedInput = resp.ModifiedInput
						modified = true
					}
				}
			}
		}
	}
	if modified {
		return modifiedInput, "", nil
	}
	return nil, "", nil
}

// RunPostToolUse executes all matching PostToolUse hooks.
// Returns the (possibly modified) output string.
func (r *Runner) RunPostToolUse(ctx context.Context, toolName string, input json.RawMessage, output string, isError bool) (string, error) {
	payload, _ := json.Marshal(PostToolInput{
		ToolName:   toolName,
		ToolInput:  input,
		ToolOutput: output,
		IsError:    isError,
	})

	result := output
	for _, group := range r.cfg.PostToolUse {
		if !matchesGroup(group.Matcher, toolName) {
			continue
		}
		for _, h := range group.Hooks {
			out, _, _, err := runCommand(ctx, h, payload, r.CWD)
			if err != nil {
				// PostToolUse errors are non-fatal
				continue
			}
			if len(out) > 0 {
				var resp PostToolOutput
				if json.Unmarshal(out, &resp) == nil && resp.ModifiedOutput != "" {
					result = resp.ModifiedOutput
				}
			}
		}
	}
	return result, nil
}

// RunNotification executes all Notification hooks with a message.
// Errors are ignored (fire-and-forget).
func (r *Runner) RunNotification(ctx context.Context, message string) {
	payload, _ := json.Marshal(NotificationInput{Message: message})
	for _, group := range r.cfg.Notification {
		for _, h := range group.Hooks {
			runCommand(ctx, h, payload, r.CWD) //nolint:errcheck
		}
	}
}

// runCommand executes a single hook command, writing payload to its stdin.
// Returns (stdout, exitNonZero, blockReason, error).
// For PreToolUse: exitNonZero=true signals the tool should be blocked.
func runCommand(ctx context.Context, h config.HookDef, payload []byte, cwd string) ([]byte, bool, string, error) {
	timeoutMs := h.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = defaultTimeoutMs
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", h.Command)
	cmd.Stdin = bytes.NewReader(payload)
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			reason := strings.TrimSpace(stdout.String())
			if reason == "" {
				reason = strings.TrimSpace(stderr.String())
			}
			return stdout.Bytes(), true, reason, nil
		}
		return nil, false, "", err
	}
	return stdout.Bytes(), false, "", nil
}
