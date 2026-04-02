package api

import (
	"encoding/json"
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient("test-key")
	if client.model != DefaultModel {
		t.Errorf("model: got %s, want %s", client.model, DefaultModel)
	}
	if client.maxTokens != DefaultMaxTokens {
		t.Errorf("maxTokens: got %d, want %d", client.maxTokens, DefaultMaxTokens)
	}
}

func TestNewClientWithOptions(t *testing.T) {
	client := NewClient("test-key",
		WithModel("claude-opus-4-6"),
		WithMaxTokens(8192),
		WithBaseURL("https://custom.api.com"),
	)
	if client.model != "claude-opus-4-6" {
		t.Errorf("model: got %s", client.model)
	}
	if client.maxTokens != 8192 {
		t.Errorf("maxTokens: got %d", client.maxTokens)
	}
}

// TestStreamResponse verifies the StreamResponse accumulator.
func TestStreamResponse(t *testing.T) {
	resp := NewStreamResponse()

	chunks := []StreamChunk{
		{Type: "message_start", Data: MessageStartEvent{
			Message: Message{ID: "msg_123", Role: "assistant", Model: "claude-sonnet-4-6"},
		}},
		{Type: "content_block_start", Data: ContentBlockStartEvent{
			Index:        0,
			ContentBlock: ContentBlock{Type: "text"},
		}},
		{Type: "content_block_delta", Data: ContentBlockDeltaEvent{
			Index: 0,
			Delta: ContentDelta{Type: "text_delta", Text: "Hello "},
		}},
		{Type: "content_block_delta", Data: ContentBlockDeltaEvent{
			Index: 0,
			Delta: ContentDelta{Type: "text_delta", Text: "World!"},
		}},
		{Type: "content_block_stop", Data: ContentBlockStopEvent{Index: 0}},
		{Type: "message_delta", Data: MessageDeltaEvent{
			Delta: MessageDelta{StopReason: StopReasonEndTurn},
			Usage: Usage{OutputTokens: 5},
		}},
	}

	for _, chunk := range chunks {
		if err := resp.ProcessChunk(chunk); err != nil {
			t.Fatalf("ProcessChunk: %v", err)
		}
	}

	if resp.Message.ID != "msg_123" {
		t.Errorf("ID: got %s", resp.Message.ID)
	}
	if len(resp.Message.Content) != 1 {
		t.Fatalf("Content: got %d blocks", len(resp.Message.Content))
	}
	if resp.Message.Content[0].Text != "Hello World!" {
		t.Errorf("Text: got %q", resp.Message.Content[0].Text)
	}
	if resp.Message.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason: got %s", resp.Message.StopReason)
	}
	if resp.Message.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens: got %d", resp.Message.Usage.OutputTokens)
	}
}

// TestStreamResponseToolUse verifies tool_use input JSON accumulation.
func TestStreamResponseToolUse(t *testing.T) {
	resp := NewStreamResponse()

	chunks := []StreamChunk{
		{Type: "content_block_start", Data: ContentBlockStartEvent{
			Index:        0,
			ContentBlock: ContentBlock{Type: "tool_use", ID: "toolu_1", Name: "bash"},
		}},
		{Type: "content_block_delta", Data: ContentBlockDeltaEvent{
			Index: 0,
			Delta: ContentDelta{Type: "input_json_delta", PartialJSON: `{"com`},
		}},
		{Type: "content_block_delta", Data: ContentBlockDeltaEvent{
			Index: 0,
			Delta: ContentDelta{Type: "input_json_delta", PartialJSON: `mand":"ls"}`},
		}},
		{Type: "content_block_stop", Data: ContentBlockStopEvent{Index: 0}},
		{Type: "message_delta", Data: MessageDeltaEvent{
			Delta: MessageDelta{StopReason: StopReasonToolUse},
		}},
	}

	for _, chunk := range chunks {
		if err := resp.ProcessChunk(chunk); err != nil {
			t.Fatalf("ProcessChunk: %v", err)
		}
	}

	if len(resp.Message.Content) != 1 {
		t.Fatalf("Content: got %d blocks", len(resp.Message.Content))
	}
	cb := resp.Message.Content[0]
	if cb.Type != "tool_use" {
		t.Errorf("Type: got %s", cb.Type)
	}
	if cb.Name != "bash" {
		t.Errorf("Name: got %s", cb.Name)
	}

	var input map[string]string
	if err := json.Unmarshal(cb.Input, &input); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}
	if input["command"] != "ls" {
		t.Errorf("command: got %s", input["command"])
	}
	if resp.Message.StopReason != StopReasonToolUse {
		t.Errorf("StopReason: got %s", resp.Message.StopReason)
	}
}

// TestStreamResponseThinking verifies thinking block accumulation.
func TestStreamResponseThinking(t *testing.T) {
	resp := NewStreamResponse()

	chunks := []StreamChunk{
		{Type: "content_block_start", Data: ContentBlockStartEvent{
			Index:        0,
			ContentBlock: ContentBlock{Type: "thinking"},
		}},
		{Type: "content_block_delta", Data: ContentBlockDeltaEvent{
			Index: 0,
			Delta: ContentDelta{Type: "thinking_delta", Thinking: "Let me think..."},
		}},
		{Type: "content_block_stop", Data: ContentBlockStopEvent{Index: 0}},
		{Type: "content_block_start", Data: ContentBlockStartEvent{
			Index:        1,
			ContentBlock: ContentBlock{Type: "text"},
		}},
		{Type: "content_block_delta", Data: ContentBlockDeltaEvent{
			Index: 1,
			Delta: ContentDelta{Type: "text_delta", Text: "Answer"},
		}},
		{Type: "content_block_stop", Data: ContentBlockStopEvent{Index: 1}},
	}

	for _, chunk := range chunks {
		if err := resp.ProcessChunk(chunk); err != nil {
			t.Fatalf("ProcessChunk: %v", err)
		}
	}

	if len(resp.Message.Content) != 2 {
		t.Fatalf("Content: got %d blocks", len(resp.Message.Content))
	}
	if resp.Message.Content[0].Type != "thinking" {
		t.Errorf("Type[0]: got %s", resp.Message.Content[0].Type)
	}
	if resp.Message.Content[0].Thinking != "Let me think..." {
		t.Errorf("Thinking: got %s", resp.Message.Content[0].Thinking)
	}
	if resp.Message.Content[1].Type != "text" {
		t.Errorf("Type[1]: got %s", resp.Message.Content[1].Type)
	}
	if resp.Message.Content[1].Text != "Answer" {
		t.Errorf("Text: got %s", resp.Message.Content[1].Text)
	}
}

// TestStreamChunkError verifies error propagation.
func TestStreamChunkError(t *testing.T) {
	resp := NewStreamResponse()
	err := resp.ProcessChunk(StreamChunk{Type: "unknown_event"})
	if err != nil {
		t.Errorf("unexpected error on unknown event: %v", err)
	}
}
