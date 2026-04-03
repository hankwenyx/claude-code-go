// Package api provides a thin wrapper around the official Anthropic Go SDK.
package api

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	DefaultModel     = "claude-sonnet-4-6"
	DefaultMaxTokens = 8096
)

// Client wraps the official Anthropic SDK client.
type Client struct {
	inner     anthropic.Client
	model     string
	maxTokens int64
}

// ClientOption configures a Client.
type ClientOption func(*clientConfig)

type clientConfig struct {
	apiKey        string
	model         string
	maxTokens     int64
	baseURL       string
	customHeaders map[string]string
}

func WithModel(model string) ClientOption       { return func(c *clientConfig) { c.model = model } }
func WithMaxTokens(n int) ClientOption          { return func(c *clientConfig) { c.maxTokens = int64(n) } }
func WithBaseURL(url string) ClientOption       { return func(c *clientConfig) { c.baseURL = url } }
func WithHTTPClient(_ interface{}) ClientOption { return func(c *clientConfig) {} } // compat shim

// WithCustomHeaders sets additional HTTP headers sent with every request.
func WithCustomHeaders(headers map[string]string) ClientOption {
	return func(c *clientConfig) { c.customHeaders = headers }
}

// NewClient creates a new Client.
// apiKey may be empty; the SDK will fall back to ANTHROPIC_API_KEY env automatically.
func NewClient(apiKey string, opts ...ClientOption) *Client {
	cfg := &clientConfig{
		apiKey:    apiKey,
		model:     DefaultModel,
		maxTokens: DefaultMaxTokens,
	}
	for _, o := range opts {
		o(cfg)
	}

	sdkOpts := []option.RequestOption{
		option.WithMaxRetries(3), // 429 / 529 / 5xx auto-retry + exponential back-off
	}
	if cfg.apiKey != "" {
		sdkOpts = append(sdkOpts, option.WithAPIKey(cfg.apiKey))
	}
	if cfg.baseURL != "" {
		sdkOpts = append(sdkOpts, option.WithBaseURL(cfg.baseURL))
	}
	for k, v := range cfg.customHeaders {
		sdkOpts = append(sdkOpts, option.WithHeader(k, v))
	}

	return &Client{
		inner:     anthropic.NewClient(sdkOpts...),
		model:     cfg.model,
		maxTokens: cfg.maxTokens,
	}
}

// StreamMessage sends a streaming request and returns a channel of StreamChunks.
// Uses the Beta endpoint (with interleaved-thinking header) only when req.Thinking is set;
// otherwise uses the standard Messages endpoint to avoid unsupported beta params on proxies.
func (c *Client) StreamMessage(ctx context.Context, req CreateMessageRequest) (<-chan StreamChunk, error) {
	initDebug()

	model := req.Model
	if model == "" {
		model = c.model
	}
	maxTok := int64(req.MaxTokens)
	if maxTok == 0 {
		maxTok = c.maxTokens
	}

	// Log outgoing request
	debugJSON(">>> request", map[string]any{
		"model":     model,
		"maxTokens": maxTok,
		"thinking":  req.Thinking != nil,
		"messages":  req.Messages,
		"system":    req.System,
		"tools":     req.Tools,
	})

	chunks := make(chan StreamChunk, 256)

	if req.Thinking != nil {
		// Use Beta endpoint for extended thinking
		params := buildParams(model, maxTok, req)
		stream := c.inner.Beta.Messages.NewStreaming(ctx, params,
			option.WithHeader("anthropic-beta", "interleaved-thinking-2025-05-14"),
		)
		go func() {
			defer close(chunks)
			for stream.Next() {
				ev := convertEvent(stream.Current())
				debugLogChunk(ev)
				chunks <- ev
			}
			if err := stream.Err(); err != nil {
				debugLog("<<< stream error: %v", err)
				chunks <- StreamChunk{Error: err}
			}
		}()
	} else {
		// Use standard endpoint — no beta query param, no thinking header
		params := buildStandardParams(model, maxTok, req)
		stream := c.inner.Messages.NewStreaming(ctx, params)
		go func() {
			defer close(chunks)
			for stream.Next() {
				ev := convertStandardEvent(stream.Current())
				debugLogChunk(ev)
				chunks <- ev
			}
			if err := stream.Err(); err != nil {
				debugLog("<<< stream error: %v", err)
				chunks <- StreamChunk{Error: err}
			}
		}()
	}

	return chunks, nil
}

// debugChunkState accumulates per-block data for richer debug output.
// Keyed by block index.
var debugChunkState = struct {
	sync.Mutex
	blockType   map[int]string // "text" | "thinking" | "tool_use"
	thinkingBuf map[int][]byte // accumulated thinking text per block
}{
	blockType:   make(map[int]string),
	thinkingBuf: make(map[int][]byte),
}

func debugLogChunk(chunk StreamChunk) {
	if !debugActive {
		return
	}
	debugChunkState.Lock()
	defer debugChunkState.Unlock()

	switch chunk.Type {
	case "message_start":
		// reset per-message state
		debugChunkState.blockType = make(map[int]string)
		debugChunkState.thinkingBuf = make(map[int][]byte)
		debugLog("<<< message_start")

	case "content_block_start":
		if ev, ok := chunk.Data.(ContentBlockStartEvent); ok {
			debugChunkState.blockType[ev.Index] = ev.ContentBlock.Type
			switch ev.ContentBlock.Type {
			case "tool_use":
				debugLog("<<< block[%d] tool_use  id=%s  name=%s", ev.Index, ev.ContentBlock.ID, ev.ContentBlock.Name)
			case "thinking":
				debugLog("<<< block[%d] thinking  (accumulating...)", ev.Index)
			default:
				debugLog("<<< block[%d] %s", ev.Index, ev.ContentBlock.Type)
			}
		}

	case "content_block_delta":
		if ev, ok := chunk.Data.(ContentBlockDeltaEvent); ok {
			switch ev.Delta.Type {
			case "text_delta":
				debugLog("<<< block[%d] text  %q", ev.Index, ev.Delta.Text)
			case "input_json_delta":
				debugLog("<<< block[%d] json  %s", ev.Index, ev.Delta.PartialJSON)
			case "thinking_delta":
				// accumulate; don't spam the log with tiny fragments
				debugChunkState.thinkingBuf[ev.Index] = append(
					debugChunkState.thinkingBuf[ev.Index],
					[]byte(ev.Delta.Thinking)...,
				)
			}
		}

	case "content_block_stop":
		if ev, ok := chunk.Data.(ContentBlockStopEvent); ok {
			bt := debugChunkState.blockType[ev.Index]
			if bt == "thinking" {
				buf := debugChunkState.thinkingBuf[ev.Index]
				debugLog("<<< block[%d] thinking  (%d chars)\n--- thinking start ---\n%s\n--- thinking end ---",
					ev.Index, len(buf), string(buf))
				delete(debugChunkState.thinkingBuf, ev.Index)
			} else {
				debugLog("<<< block[%d] stop", ev.Index)
			}
		}

	case "message_delta":
		if ev, ok := chunk.Data.(MessageDeltaEvent); ok {
			debugLog("<<< message_delta  stop_reason=%s  output_tokens=%d", ev.Delta.StopReason, ev.Usage.OutputTokens)
		}

	case "message_stop":
		debugLog("<<< message_stop")
	}
}

// buildParams converts CreateMessageRequest → SDK BetaMessageNewParams.
func buildParams(model string, maxTokens int64, req CreateMessageRequest) anthropic.BetaMessageNewParams {
	p := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
	}

	// System blocks
	for _, sb := range req.System {
		block := anthropic.BetaTextBlockParam{Text: sb.Text}
		if sb.CacheControl != nil {
			block.CacheControl = anthropic.BetaCacheControlEphemeralParam{}
		}
		p.System = append(p.System, block)
	}

	// Messages
	for _, m := range req.Messages {
		p.Messages = append(p.Messages, convertAPIMessage(m))
	}

	// Tools
	for _, td := range req.Tools {
		var betaProps any
		var betaReq []string
		var raw map[string]json.RawMessage
		if json.Unmarshal(td.InputSchema, &raw) == nil {
			if props, ok := raw["properties"]; ok {
				betaProps = props
			}
			if req2, ok := raw["required"]; ok {
				json.Unmarshal(req2, &betaReq)
			}
		}
		p.Tools = append(p.Tools, anthropic.BetaToolUnionParam{
			OfTool: &anthropic.BetaToolParam{
				Name:        td.Name,
				Description: anthropic.String(td.Description),
				InputSchema: anthropic.BetaToolInputSchemaParam{
					Properties: betaProps,
					Required:   betaReq,
				},
			},
		})
	}

	// Thinking
	if req.Thinking != nil {
		budget := int64(req.Thinking.BudgetTokens)
		if budget == 0 {
			budget = 8000 // default for "adaptive"
		}
		p.Thinking = anthropic.BetaThinkingConfigParamUnion{
			OfEnabled: &anthropic.BetaThinkingConfigEnabledParam{
				Type:         "enabled",
				BudgetTokens: budget,
			},
		}
	}

	return p
}

// convertAPIMessage converts our APIMessage to the SDK's BetaMessageParam.
func convertAPIMessage(m APIMessage) anthropic.BetaMessageParam {
	var blocks []anthropic.BetaContentBlockParamUnion

	// Content is json.RawMessage: try []ContentBlock first, then plain string,
	// then []ToolResultBlock (for tool results injected back by agent loop).
	// NOTE: ToolResultBlock also has a "type" field ("tool_result"), so we must
	// check the type before treating as ContentBlock to avoid losing tool results.
	var contentBlocks []ContentBlock
	if err := json.Unmarshal(m.Content, &contentBlocks); err == nil && len(contentBlocks) > 0 &&
		contentBlocks[0].Type != "tool_result" {
		for _, cb := range contentBlocks {
			blocks = append(blocks, contentBlockToSDKParam(cb))
		}
	} else {
		// Try tool result blocks
		var toolResults []ToolResultBlock
		if err := json.Unmarshal(m.Content, &toolResults); err == nil && len(toolResults) > 0 {
			for _, tr := range toolResults {
				isErr := tr.IsError
				blocks = append(blocks, anthropic.BetaContentBlockParamUnion{
					OfToolResult: &anthropic.BetaToolResultBlockParam{
						ToolUseID: tr.ToolUseID,
						Content: []anthropic.BetaToolResultBlockParamContentUnion{{
							OfText: &anthropic.BetaTextBlockParam{Text: tr.Content},
						}},
						IsError: anthropic.Bool(isErr),
					},
				})
			}
		} else {
			// Plain string fallback
			var text string
			if json.Unmarshal(m.Content, &text) == nil {
				blocks = append(blocks, anthropic.BetaContentBlockParamUnion{
					OfText: &anthropic.BetaTextBlockParam{Text: text},
				})
			}
		}
	}

	return anthropic.BetaMessageParam{
		Role:    anthropic.BetaMessageParamRole(m.Role),
		Content: blocks,
	}
}

func contentBlockToSDKParam(cb ContentBlock) anthropic.BetaContentBlockParamUnion {
	switch cb.Type {
	case "tool_use":
		return anthropic.BetaContentBlockParamUnion{
			OfToolUse: &anthropic.BetaToolUseBlockParam{
				ID:    cb.ID,
				Name:  cb.Name,
				Input: cb.Input,
			},
		}
	case "thinking":
		return anthropic.BetaContentBlockParamUnion{
			OfThinking: &anthropic.BetaThinkingBlockParam{
				Type:      "thinking",
				Thinking:  cb.Thinking,
				Signature: cb.Text,
			},
		}
	case "redacted_thinking":
		return anthropic.BetaContentBlockParamUnion{
			OfRedactedThinking: &anthropic.BetaRedactedThinkingBlockParam{
				Type: "redacted_thinking",
				Data: cb.Text,
			},
		}
	default: // "text" and everything else
		return anthropic.BetaContentBlockParamUnion{
			OfText: &anthropic.BetaTextBlockParam{Text: cb.Text},
		}
	}
}

// convertEvent bridges SDK BetaRawMessageStreamEventUnion → our StreamChunk.
func convertEvent(ev anthropic.BetaRawMessageStreamEventUnion) StreamChunk {
	switch v := ev.AsAny().(type) {
	case anthropic.BetaRawMessageStartEvent:
		msg := Message{
			ID:    v.Message.ID,
			Type:  "message",
			Role:  string(v.Message.Role),
			Model: string(v.Message.Model),
		}
		return StreamChunk{Type: "message_start", Data: MessageStartEvent{Message: msg}}

	case anthropic.BetaRawContentBlockStartEvent:
		cb := ContentBlock{}
		switch inner := v.ContentBlock.AsAny().(type) {
		case anthropic.BetaToolUseBlock:
			cb.Type = "tool_use"
			cb.ID = inner.ID
			cb.Name = inner.Name
		case anthropic.BetaThinkingBlock:
			cb.Type = "thinking"
		default:
			cb.Type = strings.ToLower(string(v.ContentBlock.Type))
		}
		return StreamChunk{Type: "content_block_start", Data: ContentBlockStartEvent{
			Index:        int(v.Index),
			ContentBlock: cb,
		}}

	case anthropic.BetaRawContentBlockDeltaEvent:
		var delta ContentDelta
		switch d := v.Delta.AsAny().(type) {
		case anthropic.BetaTextDelta:
			delta = ContentDelta{Type: "text_delta", Text: d.Text}
		case anthropic.BetaInputJSONDelta:
			delta = ContentDelta{Type: "input_json_delta", PartialJSON: d.PartialJSON}
		case anthropic.BetaThinkingDelta:
			delta = ContentDelta{Type: "thinking_delta", Thinking: d.Thinking}
		}
		return StreamChunk{Type: "content_block_delta", Data: ContentBlockDeltaEvent{
			Index: int(v.Index),
			Delta: delta,
		}}

	case anthropic.BetaRawContentBlockStopEvent:
		return StreamChunk{Type: "content_block_stop", Data: ContentBlockStopEvent{
			Index: int(v.Index),
		}}

	case anthropic.BetaRawMessageDeltaEvent:
		return StreamChunk{Type: "message_delta", Data: MessageDeltaEvent{
			Delta: MessageDelta{StopReason: StopReason(v.Delta.StopReason)},
			Usage: Usage{OutputTokens: int(v.Usage.OutputTokens)},
		}}

	case anthropic.BetaRawMessageStopEvent:
		return StreamChunk{Type: "message_stop"}
	}

	return StreamChunk{Type: strings.ToLower(ev.Type)}
}

// buildStandardParams builds MessageNewParams for the non-beta endpoint.
func buildStandardParams(model string, maxTokens int64, req CreateMessageRequest) anthropic.MessageNewParams {
	p := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
	}

	// System blocks
	for _, sb := range req.System {
		p.System = append(p.System, anthropic.TextBlockParam{Text: sb.Text})
	}

	// Messages
	for _, m := range req.Messages {
		p.Messages = append(p.Messages, convertAPIMessageStandard(m))
	}

	// Tools
	for _, td := range req.Tools {
		p.Tools = append(p.Tools, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        td.Name,
				Description: anthropic.String(td.Description),
				InputSchema: schemaToStandardParam(td.InputSchema),
			},
		})
	}

	return p
}

// schemaToStandardParam converts a full JSON schema RawMessage into ToolInputSchemaParam.
// The schema is expected to be an object with optional "properties" and "required" fields.
func schemaToStandardParam(schema json.RawMessage) anthropic.ToolInputSchemaParam {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(schema, &raw); err != nil {
		return anthropic.ToolInputSchemaParam{Properties: json.RawMessage(schema)}
	}
	p := anthropic.ToolInputSchemaParam{}
	if props, ok := raw["properties"]; ok {
		p.Properties = props
	}
	if req, ok := raw["required"]; ok {
		var required []string
		if json.Unmarshal(req, &required) == nil {
			p.Required = required
		}
	}
	return p
}

func convertAPIMessageStandard(m APIMessage) anthropic.MessageParam {
	var blocks []anthropic.ContentBlockParamUnion

	// NOTE: ToolResultBlock also has a "type" field ("tool_result"), so we must
	// check the type before treating as ContentBlock to avoid losing tool results.
	var contentBlocks []ContentBlock
	if err := json.Unmarshal(m.Content, &contentBlocks); err == nil && len(contentBlocks) > 0 &&
		contentBlocks[0].Type != "tool_result" {
		for _, cb := range contentBlocks {
			if cb.Type == "tool_use" {
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    cb.ID,
						Name:  cb.Name,
						Input: cb.Input,
					},
				})
			} else {
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfText: &anthropic.TextBlockParam{Text: cb.Text},
				})
			}
		}
	} else {
		var toolResults []ToolResultBlock
		if err := json.Unmarshal(m.Content, &toolResults); err == nil && len(toolResults) > 0 {
			for _, tr := range toolResults {
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolResult: &anthropic.ToolResultBlockParam{
						ToolUseID: tr.ToolUseID,
						Content: []anthropic.ToolResultBlockParamContentUnion{{
							OfText: &anthropic.TextBlockParam{Text: tr.Content},
						}},
						IsError: anthropic.Bool(tr.IsError),
					},
				})
			}
		} else {
			var text string
			if json.Unmarshal(m.Content, &text) == nil {
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfText: &anthropic.TextBlockParam{Text: text},
				})
			}
		}
	}

	return anthropic.MessageParam{
		Role:    anthropic.MessageParamRole(m.Role),
		Content: blocks,
	}
}

// convertStandardEvent bridges SDK MessageStreamEventUnion → our StreamChunk.
func convertStandardEvent(ev anthropic.MessageStreamEventUnion) StreamChunk {
	switch v := ev.AsAny().(type) {
	case anthropic.MessageStartEvent:
		msg := Message{
			ID:    v.Message.ID,
			Type:  "message",
			Role:  string(v.Message.Role),
			Model: string(v.Message.Model),
		}
		return StreamChunk{Type: "message_start", Data: MessageStartEvent{Message: msg}}

	case anthropic.ContentBlockStartEvent:
		cb := ContentBlock{}
		switch inner := v.ContentBlock.AsAny().(type) {
		case anthropic.ToolUseBlock:
			cb.Type = "tool_use"
			cb.ID = inner.ID
			cb.Name = inner.Name
		default:
			cb.Type = strings.ToLower(string(v.ContentBlock.Type))
		}
		return StreamChunk{Type: "content_block_start", Data: ContentBlockStartEvent{
			Index:        int(v.Index),
			ContentBlock: cb,
		}}

	case anthropic.ContentBlockDeltaEvent:
		var delta ContentDelta
		switch d := v.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			delta = ContentDelta{Type: "text_delta", Text: d.Text}
		case anthropic.InputJSONDelta:
			delta = ContentDelta{Type: "input_json_delta", PartialJSON: d.PartialJSON}
		}
		return StreamChunk{Type: "content_block_delta", Data: ContentBlockDeltaEvent{
			Index: int(v.Index),
			Delta: delta,
		}}

	case anthropic.ContentBlockStopEvent:
		return StreamChunk{Type: "content_block_stop", Data: ContentBlockStopEvent{
			Index: int(v.Index),
		}}

	case anthropic.MessageDeltaEvent:
		return StreamChunk{Type: "message_delta", Data: MessageDeltaEvent{
			Delta: MessageDelta{StopReason: StopReason(v.Delta.StopReason)},
			Usage: Usage{OutputTokens: int(v.Usage.OutputTokens)},
		}}

	case anthropic.MessageStopEvent:
		return StreamChunk{Type: "message_stop"}
	}

	return StreamChunk{Type: strings.ToLower(ev.Type)}
}
