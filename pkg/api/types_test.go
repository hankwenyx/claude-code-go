package api

import (
	"encoding/json"
	"testing"
)

func TestContentBlock(t *testing.T) {
	tests := []struct {
		name     string
		block    ContentBlock
		wantJSON string
	}{
		{
			name:     "text block",
			block:    ContentBlock{Type: "text", Text: "hello"},
			wantJSON: `{"type":"text","text":"hello"}`,
		},
		{
			name:     "tool_use block",
			block:    ContentBlock{Type: "tool_use", ID: "123", Name: "bash", Input: json.RawMessage(`{"cmd":"ls"}`)},
			wantJSON: `{"type":"tool_use","id":"123","name":"bash","input":{"cmd":"ls"}}`,
		},
		{
			name:     "thinking block",
			block:    ContentBlock{Type: "thinking", Thinking: "let me think..."},
			wantJSON: `{"type":"thinking","thinking":"let me think..."}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.block)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Errorf("got %s, want %s", got, tt.wantJSON)
			}
		})
	}
}

func TestToolResultBlock(t *testing.T) {
	tests := []struct {
		name     string
		block    ToolResultBlock
		wantJSON string
	}{
		{
			name:     "success result",
			block:    ToolResultBlock{Type: "tool_result", ToolUseID: "123", Content: "output"},
			wantJSON: `{"type":"tool_result","tool_use_id":"123","content":"output"}`,
		},
		{
			name:     "error result",
			block:    ToolResultBlock{Type: "tool_result", ToolUseID: "123", Content: "error", IsError: true},
			wantJSON: `{"type":"tool_result","tool_use_id":"123","content":"error","is_error":true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.block)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Errorf("got %s, want %s", got, tt.wantJSON)
			}
		})
	}
}

func TestAPIMessage(t *testing.T) {
	t.Run("user text message", func(t *testing.T) {
		msg := NewUserTextMessage("hello")
		if msg.Role != "user" {
			t.Errorf("got role %s, want user", msg.Role)
		}

		var content []ContentBlock
		if err := json.Unmarshal(msg.Content, &content); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if len(content) != 1 || content[0].Text != "hello" {
			t.Errorf("unexpected content: %v", content)
		}
	})

	t.Run("assistant text message", func(t *testing.T) {
		msg := NewAssistantTextMessage("hi there")
		if msg.Role != "assistant" {
			t.Errorf("got role %s, want assistant", msg.Role)
		}

		var content []ContentBlock
		if err := json.Unmarshal(msg.Content, &content); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if len(content) != 1 || content[0].Text != "hi there" {
			t.Errorf("unexpected content: %v", content)
		}
	})
}

func TestMessage(t *testing.T) {
	msg := Message{
		ID:         "msg_123",
		Type:       "message",
		Role:       "assistant",
		Content:    []ContentBlock{{Type: "text", Text: "hello"}},
		Model:      "claude-sonnet-4-6",
		StopReason: StopReasonEndTurn,
		Usage:      Usage{InputTokens: 10, OutputTokens: 5},
	}

	got, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var unmarshaled Message
	if err := json.Unmarshal(got, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if unmarshaled.ID != msg.ID {
		t.Errorf("ID: got %s, want %s", unmarshaled.ID, msg.ID)
	}
	if unmarshaled.StopReason != msg.StopReason {
		t.Errorf("StopReason: got %s, want %s", unmarshaled.StopReason, msg.StopReason)
	}
}

func TestSystemBlock(t *testing.T) {
	t.Run("without cache control", func(t *testing.T) {
		block := SystemBlock{Type: "text", Text: "system prompt"}
		got, err := json.Marshal(block)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		want := `{"type":"text","text":"system prompt"}`
		if string(got) != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})

	t.Run("with cache control", func(t *testing.T) {
		block := SystemBlock{
			Type:         "text",
			Text:         "system prompt",
			CacheControl: &CacheControl{Type: "ephemeral"},
		}
		got, err := json.Marshal(block)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		want := `{"type":"text","text":"system prompt","cache_control":{"type":"ephemeral"}}`
		if string(got) != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
}

func TestCreateMessageRequest(t *testing.T) {
	req := CreateMessageRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 4096,
		Messages:  []APIMessage{NewUserTextMessage("hello")},
		System:    []SystemBlock{{Type: "text", Text: "you are helpful"}},
		Stream:    true,
	}

	got, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var unmarshaled CreateMessageRequest
	if err := json.Unmarshal(got, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if unmarshaled.Model != req.Model {
		t.Errorf("Model: got %s, want %s", unmarshaled.Model, req.Model)
	}
	if unmarshaled.MaxTokens != req.MaxTokens {
		t.Errorf("MaxTokens: got %d, want %d", unmarshaled.MaxTokens, req.MaxTokens)
	}
	if !unmarshaled.Stream {
		t.Error("Stream should be true")
	}
}

func TestThinkingConfig(t *testing.T) {
	t.Run("adaptive", func(t *testing.T) {
		config := ThinkingConfig{Type: "adaptive"}
		got, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		want := `{"type":"adaptive"}`
		if string(got) != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})

	t.Run("enabled with budget", func(t *testing.T) {
		config := ThinkingConfig{Type: "enabled", BudgetTokens: 10000}
		got, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		want := `{"type":"enabled","budget_tokens":10000}`
		if string(got) != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
}

func TestErrorResponse(t *testing.T) {
	err := &ErrorResponse{Type: "invalid_request_error", Message: "bad request"}

	if err.Error() != "invalid_request_error: bad request" {
		t.Errorf("Error(): got %s", err.Error())
	}

	if err.IsRetryable() {
		t.Error("invalid_request_error should not be retryable")
	}

	retryable := &ErrorResponse{Type: "overloaded_error", Message: "overloaded"}
	if !retryable.IsRetryable() {
		t.Error("overloaded_error should be retryable")
	}
}

func TestStreamEvents(t *testing.T) {
	t.Run("message_start", func(t *testing.T) {
		event := MessageStartEvent{
			Type: "message_start",
			Message: Message{
				ID:    "msg_123",
				Role:  "assistant",
				Model: "claude-sonnet-4-6",
			},
		}
		got, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		var unmarshaled MessageStartEvent
		if err := json.Unmarshal(got, &unmarshaled); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if unmarshaled.Message.ID != "msg_123" {
			t.Errorf("Message.ID: got %s", unmarshaled.Message.ID)
		}
	})

	t.Run("content_block_delta", func(t *testing.T) {
		event := ContentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: 0,
			Delta: ContentDelta{Type: "text_delta", Text: "hello"},
		}
		got, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		var unmarshaled ContentBlockDeltaEvent
		if err := json.Unmarshal(got, &unmarshaled); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if unmarshaled.Delta.Text != "hello" {
			t.Errorf("Delta.Text: got %s", unmarshaled.Delta.Text)
		}
	})
}
