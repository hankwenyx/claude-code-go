// Package api provides Anthropic API client and types
package api

import "encoding/json"

// StopReason indicates why the model stopped generating
type StopReason string

const (
	StopReasonEndTurn   StopReason = "end_turn"
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonToolUse   StopReason = "tool_use"
	StopReasonPauseTurn StopReason = "pause_turn"
)

// ContentBlock represents a content block in a message
type ContentBlock struct {
	Type     string          `json:"type"`               // "text" | "tool_use" | "thinking" | "redacted_thinking"
	Text     string          `json:"text,omitempty"`     // text block
	ID       string          `json:"id,omitempty"`       // tool_use block
	Name     string          `json:"name,omitempty"`     // tool_use block
	Input    json.RawMessage `json:"input,omitempty"`    // tool_use block
	Thinking string          `json:"thinking,omitempty"` // thinking block
}

// ToolResultBlock represents a tool result in a user message
type ToolResultBlock struct {
	Type      string `json:"type"`                // "tool_result"
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// APIMessage represents a message sent to/received from the API
type APIMessage struct {
	Role    string          `json:"role"`    // "user" | "assistant"
	Content json.RawMessage `json:"content"` // Can be string or []ContentBlock
}

// NewTextContent creates a text content array
func NewTextContent(text string) []ContentBlock {
	return []ContentBlock{{Type: "text", Text: text}}
}

// NewUserTextMessage creates a user message with text content
func NewUserTextMessage(text string) APIMessage {
	content, _ := json.Marshal(NewTextContent(text))
	return APIMessage{Role: "user", Content: content}
}

// NewAssistantTextMessage creates an assistant message with text content
func NewAssistantTextMessage(text string) APIMessage {
	content, _ := json.Marshal(NewTextContent(text))
	return APIMessage{Role: "assistant", Content: content}
}

// Message represents a complete response from the API
type Message struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"` // "message"
	Role       string         `json:"role"` // "assistant"
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason StopReason     `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

// Usage represents token usage information
type Usage struct {
	InputTokens          int `json:"input_tokens"`
	OutputTokens         int `json:"output_tokens"`
	CacheReadInputTokens int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationTokens  int `json:"cache_creation_tokens,omitempty"`
}

// SystemBlock represents a block in the system prompt
type SystemBlock struct {
	Type         string        `json:"type"` // "text"
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// CacheControl specifies caching behavior
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// ToolDef represents a tool definition for the API
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"` // JSON Schema
}

// ThinkingConfig specifies thinking behavior
type ThinkingConfig struct {
	Type         string `json:"type"`                    // "adaptive" | "enabled" | "disabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // when type="enabled"
}

// CreateMessageRequest represents a request to create a message
type CreateMessageRequest struct {
	Model     string          `json:"model"`
	Messages  []APIMessage    `json:"messages"`
	System    []SystemBlock   `json:"system,omitempty"`
	Tools     []ToolDef       `json:"tools,omitempty"`
	MaxTokens int             `json:"max_tokens"`
	Thinking  *ThinkingConfig `json:"thinking,omitempty"`
	Stream    bool            `json:"stream"`
}

// StreamChunk represents a chunk from the streaming API
type StreamChunk struct {
	Type  string      `json:"type"`
	Data  interface{} `json:"data,omitempty"`
	Error error        `json:"error,omitempty"`
}

// MessageStartEvent represents a message_start event
type MessageStartEvent struct {
	Type    string  `json:"type"`
	Message Message `json:"message"`
}

// ContentBlockStartEvent represents a content_block_start event
type ContentBlockStartEvent struct {
	Type         string        `json:"type"`
	Index        int           `json:"index"`
	ContentBlock ContentBlock  `json:"content_block"`
}

// ContentBlockDeltaEvent represents a content_block_delta event
type ContentBlockDeltaEvent struct {
	Type  string       `json:"type"`
	Index int          `json:"index"`
	Delta ContentDelta `json:"delta"`
}

// ContentDelta represents a delta in streaming
type ContentDelta struct {
	Type        string `json:"type"`                   // "text_delta" | "input_json_delta" | "thinking_delta"
	Text        string `json:"text,omitempty"`         // for text_delta
	PartialJSON string `json:"partial_json,omitempty"` // for input_json_delta
	Thinking    string `json:"thinking,omitempty"`     // for thinking_delta
}

// ContentBlockStopEvent represents a content_block_stop event
type ContentBlockStopEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// MessageDeltaEvent represents a message_delta event
type MessageDeltaEvent struct {
	Type       string        `json:"type"`
	Usage      Usage         `json:"usage,omitempty"`
	Delta      MessageDelta  `json:"delta,omitempty"`
}

// MessageDelta represents delta in message
type MessageDelta struct {
	Type       string     `json:"type"`
	StopReason StopReason `json:"stop_reason,omitempty"`
}

// StreamResponse accumulates a streaming response into a complete Message.
type StreamResponse struct {
	Message    Message
	blockIndex map[int]int // maps stream index → content slice index
	jsonBufs   map[int]*[]byte
}

// NewStreamResponse creates an empty StreamResponse ready to receive chunks.
func NewStreamResponse() *StreamResponse {
	return &StreamResponse{
		blockIndex: make(map[int]int),
		jsonBufs:   make(map[int]*[]byte),
	}
}

// ProcessChunk incorporates one StreamChunk into the accumulated Message.
func (sr *StreamResponse) ProcessChunk(chunk StreamChunk) error {
	switch chunk.Type {
	case "message_start":
		if ev, ok := chunk.Data.(MessageStartEvent); ok {
			sr.Message = ev.Message
		}
	case "content_block_start":
		if ev, ok := chunk.Data.(ContentBlockStartEvent); ok {
			idx := len(sr.Message.Content)
			sr.blockIndex[ev.Index] = idx
			sr.Message.Content = append(sr.Message.Content, ev.ContentBlock)
			if ev.ContentBlock.Type == "tool_use" {
				buf := make([]byte, 0, 256)
				sr.jsonBufs[ev.Index] = &buf
			}
		}
	case "content_block_delta":
		if ev, ok := chunk.Data.(ContentBlockDeltaEvent); ok {
			idx, ok2 := sr.blockIndex[ev.Index]
			if !ok2 || idx >= len(sr.Message.Content) {
				break
			}
			switch ev.Delta.Type {
			case "text_delta":
				sr.Message.Content[idx].Text += ev.Delta.Text
			case "thinking_delta":
				sr.Message.Content[idx].Thinking += ev.Delta.Thinking
			case "input_json_delta":
				if buf, ok3 := sr.jsonBufs[ev.Index]; ok3 {
					*buf = append(*buf, ev.Delta.PartialJSON...)
				}
			}
		}
	case "content_block_stop":
		if ev, ok := chunk.Data.(ContentBlockStopEvent); ok {
			if buf, ok2 := sr.jsonBufs[ev.Index]; ok2 {
				if idx, ok3 := sr.blockIndex[ev.Index]; ok3 && idx < len(sr.Message.Content) {
					sr.Message.Content[idx].Input = json.RawMessage(*buf)
				}
			}
		}
	case "message_delta":
		if ev, ok := chunk.Data.(MessageDeltaEvent); ok {
			sr.Message.StopReason = ev.Delta.StopReason
			sr.Message.Usage.OutputTokens = ev.Usage.OutputTokens
		}
	}
	return nil
}

// ErrorResponse represents an API error
type ErrorResponse struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Error implements the error interface
func (e *ErrorResponse) Error() string {
	return e.Type + ": " + e.Message
}

// IsRetryable returns true if the error is retryable
func (e *ErrorResponse) IsRetryable() bool {
	return e.Type == "overloaded_error" || e.Type == "rate_limit_error"
}
