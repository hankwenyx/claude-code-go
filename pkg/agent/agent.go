package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hankwenyx/claude-code-go/pkg/api"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

// RunAgent runs the agent loop and returns a channel of events
func RunAgent(ctx context.Context, initialMessage string, opts AgentOptions) <-chan AgentEvent {
	events := make(chan AgentEvent, 100)

	go func() {
		defer close(events)
		runLoop(ctx, initialMessage, opts, events)
	}()

	return events
}

func runLoop(ctx context.Context, initialMessage string, opts AgentOptions, events chan<- AgentEvent) {
	// Ensure session ID
	if opts.SessionID == "" {
		opts.SessionID = fmt.Sprintf("%x", rand.Int63())
	}
	// Ensure CWD
	if opts.CWD == "" {
		opts.CWD, _ = os.Getwd()
	}

	// Create client if not provided
	client := opts.Client
	if client == nil {
		clientOpts := []api.ClientOption{}
		if opts.Model != "" {
			clientOpts = append(clientOpts, api.WithModel(opts.Model))
		}
		if opts.MaxTokens > 0 {
			clientOpts = append(clientOpts, api.WithMaxTokens(opts.MaxTokens))
		}
		if opts.APIBaseURL != "" {
			clientOpts = append(clientOpts, api.WithBaseURL(opts.APIBaseURL))
		}
		client = api.NewClient(opts.APIKey, clientOpts...)
	}

	// Build tool definitions
	var toolDefs []api.ToolDef
	if opts.Registry != nil {
		toolDefs = opts.Registry.ToAPIDefs()
	}

	// Build conversation history.
	// Merge CLAUDE.md into the first user message to avoid consecutive user messages,
	// which some API proxies (e.g. Qianfan/glm-5) do not support.
	firstMsg := initialMessage
	if opts.ClaudeMdContent != "" {
		firstMsg = "<system-reminder>\n" + opts.ClaudeMdContent + "\n</system-reminder>\n\n" + initialMessage
	}
	messages := []api.APIMessage{api.NewUserTextMessage(firstMsg)}

	maxTurns := opts.MaxTurns
	if maxTurns == 0 {
		maxTurns = 50 // default safety limit
	}

	for turn := 0; turn < maxTurns; turn++ {
		// Build request
		req := api.CreateMessageRequest{
			Model:    opts.Model,
			Messages: messages,
			System:   opts.SystemPrompt,
			Tools:    toolDefs,
		}
		if opts.MaxTokens > 0 {
			req.MaxTokens = opts.MaxTokens
		}
		if isThinkingModel(req.Model) {
			req.Thinking = &api.ThinkingConfig{Type: "adaptive"}
		}

		// Stream the response
		chunks, err := client.StreamMessage(ctx, req)
		if err != nil {
			events <- AgentEvent{Type: EventError, Error: err}
			return
		}

		// Process stream and collect tool calls
		resp := api.NewStreamResponse()
		needsFollowUp := false

		for chunk := range chunks {
			if chunk.Error != nil {
				events <- AgentEvent{Type: EventError, Error: chunk.Error}
				return
			}

			if err := resp.ProcessChunk(chunk); err != nil {
				events <- AgentEvent{Type: EventError, Error: err}
				return
			}

			// Emit text deltas immediately
			if chunk.Type == "content_block_delta" {
				if delta, ok := chunk.Data.(api.ContentBlockDeltaEvent); ok {
					if delta.Delta.Type == "text_delta" {
						events <- AgentEvent{Type: EventText, Text: delta.Delta.Text}
					}
				}
			}

			// Detect tool_use to set needsFollowUp flag
			if chunk.Type == "content_block_start" {
				if start, ok := chunk.Data.(api.ContentBlockStartEvent); ok {
					if start.ContentBlock.Type == "tool_use" {
						needsFollowUp = true
					}
				}
			}
		}

		// Append assistant message to history
		assistantContent, _ := json.Marshal(resp.Message.Content)
		messages = append(messages, api.APIMessage{
			Role:    "assistant",
			Content: json.RawMessage(assistantContent),
		})

		if !needsFollowUp {
			// Done
			events <- AgentEvent{Type: EventMessage, Message: &resp.Message}
			return
		}

		// Execute tools and build tool_result user message
		toolResults := executeTools(ctx, resp.Message.Content, opts, events)

		// Append tool results as a single user message
		toolResultsJSON, _ := json.Marshal(toolResults)
		messages = append(messages, api.APIMessage{
			Role:    "user",
			Content: json.RawMessage(toolResultsJSON),
		})
	}

	// Max turns exceeded
	events <- AgentEvent{
		Type:  EventError,
		Error: &maxTurnsError{maxTurns},
	}
}

type maxTurnsError struct{ max int }

func (e *maxTurnsError) Error() string {
	return "max turns exceeded"
}

// executeTools executes all tool calls from the assistant message.
// Read-only tools run concurrently; write tools run serially as barriers.
func executeTools(ctx context.Context, content []api.ContentBlock, opts AgentOptions, events chan<- AgentEvent) []api.ToolResultBlock {
	var toolUses []api.ContentBlock
	for _, block := range content {
		if block.Type == "tool_use" {
			toolUses = append(toolUses, block)
		}
	}

	if len(toolUses) == 0 {
		return nil
	}

	results := make([]api.ToolResultBlock, len(toolUses))

	// Group consecutive read-only tools together; write tools flush pending group
	type group struct {
		readOnly bool
		indices  []int
	}
	var groups []group
	for i, tu := range toolUses {
		var t tools.Tool
		if opts.Registry != nil {
			t = opts.Registry.Get(tu.Name)
		}
		isRO := t != nil && t.IsReadOnly()

		if len(groups) == 0 || groups[len(groups)-1].readOnly != isRO || !isRO {
			groups = append(groups, group{readOnly: isRO, indices: []int{i}})
		} else {
			groups[len(groups)-1].indices = append(groups[len(groups)-1].indices, i)
		}
	}

	for _, g := range groups {
		if g.readOnly && len(g.indices) > 1 {
			// Concurrent execution for read-only tools
			var wg sync.WaitGroup
			for _, idx := range g.indices {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					results[i] = callTool(ctx, toolUses[i], opts, events)
				}(idx)
			}
			wg.Wait()
		} else {
			// Serial execution
			for _, idx := range g.indices {
				results[idx] = callTool(ctx, toolUses[idx], opts, events)
			}
		}
	}

	return results
}

func callTool(ctx context.Context, tu api.ContentBlock, opts AgentOptions, events chan<- AgentEvent) api.ToolResultBlock {
	// Emit tool use event
	events <- AgentEvent{
		Type: EventToolUse,
		ToolCall: &ToolCall{
			ID:    tu.ID,
			Name:  tu.Name,
			Input: []byte(tu.Input),
		},
	}

	var t tools.Tool
	if opts.Registry != nil {
		t = opts.Registry.Get(tu.Name)
	}

	if t == nil {
		result := api.ToolResultBlock{
			Type:      "tool_result",
			ToolUseID: tu.ID,
			Content:   "tool not found: " + tu.Name,
			IsError:   true,
		}
		events <- AgentEvent{
			Type: EventToolResult,
			ToolResult: &ToolResult{
				ToolUseID: tu.ID,
				Content:   result.Content,
				IsError:   true,
			},
		}
		return result
	}

	// Permission check
	if opts.PermChecker != nil {
		decision := opts.PermChecker.Check(t, tu.Input)
		if decision.Behavior == "deny" {
			content := "permission denied: " + decision.Reason
			result := api.ToolResultBlock{
				Type:      "tool_result",
				ToolUseID: tu.ID,
				Content:   content,
				IsError:   true,
			}
			events <- AgentEvent{
				Type: EventToolResult,
				ToolResult: &ToolResult{
					ToolUseID: tu.ID,
					Content:   content,
					IsError:   true,
				},
			}
			return result
		}
	}

	// Execute tool
	toolResult, err := t.Call(ctx, tu.Input)
	content := toolResult.Content
	isError := toolResult.IsError
	if err != nil {
		content = err.Error()
		isError = true
	}

	// Truncate / persist large results
	if len(content) > tools.DefaultMaxResultSize {
		content = persistToolResult(tu.ID, content, opts)
	}

	events <- AgentEvent{
		Type: EventToolResult,
		ToolResult: &ToolResult{
			ToolUseID: tu.ID,
			Content:   content,
			IsError:   isError,
		},
	}

	return api.ToolResultBlock{
		Type:      "tool_result",
		ToolUseID: tu.ID,
		Content:   content,
		IsError:   isError,
	}
}

// RunAgentSync runs the agent synchronously and returns the final text
func RunAgentSync(ctx context.Context, initialMessage string, opts AgentOptions) (string, error) {
	events := RunAgent(ctx, initialMessage, opts)

	var text strings.Builder
	var finalErr error

	for event := range events {
		switch event.Type {
		case EventText:
			text.WriteString(event.Text)
		case EventError:
			finalErr = event.Error
		case EventMessage:
			// Final message received
		}
	}

	return text.String(), finalErr
}

// isThinkingModel checks if the model supports thinking
func isThinkingModel(model string) bool {
	return strings.Contains(model, "claude-sonnet-4") ||
		strings.Contains(model, "claude-opus-4")
}

// persistToolResult writes large tool output to disk and returns a compact reference string.
// Path: ~/.claude/projects/{cwdSlug}/{sessionID}/tool-results/{toolUseID}.txt
func persistToolResult(toolUseID, content string, opts AgentOptions) string {
	const previewSize = 2000

	preview := content
	if len(preview) > previewSize {
		preview = preview[:previewSize]
	}

	// Attempt to write full content to disk
	if path, err := writeResultToDisk(toolUseID, content, opts); err == nil {
		return fmt.Sprintf(
			"<persisted-output file=%q>\nOutput too large (%d bytes). Preview (first 2 KB):\n%s\n</persisted-output>",
			path, len(content), preview,
		)
	}

	// Disk write failed: inline truncation fallback
	return fmt.Sprintf(
		"<persisted-output>\nOutput too large (%d bytes). Preview (first 2 KB):\n%s\n</persisted-output>",
		len(content), preview,
	)
}

func writeResultToDisk(toolUseID, content string, opts AgentOptions) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Build slug from CWD: replace path separators with '-'
	cwd := opts.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	slug := strings.NewReplacer("/", "-", "\\", "-", ":", "").Replace(cwd)
	slug = strings.TrimPrefix(slug, "-")

	dir := filepath.Join(home, ".claude", "projects", slug, opts.SessionID, "tool-results")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, toolUseID+".txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}
