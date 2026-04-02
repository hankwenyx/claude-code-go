package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/api"
)

func TestRunAgent(t *testing.T) {
	// Mock SSE response
	sseResponse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":"","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello, "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world!"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {}
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	client := api.NewClient("test-key", api.WithBaseURL(server.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := RunAgent(ctx, "hello", AgentOptions{
		Client: client,
	})

	var textEvents []string
	var finalMessage *api.Message

	for event := range events {
		switch event.Type {
		case EventText:
			textEvents = append(textEvents, event.Text)
		case EventMessage:
			finalMessage = event.Message
		case EventError:
			t.Fatalf("unexpected error: %v", event.Error)
		}
	}

	// Check text events
	if len(textEvents) != 2 {
		t.Errorf("expected 2 text events, got %d", len(textEvents))
	}
	if textEvents[0] != "Hello, " || textEvents[1] != "world!" {
		t.Errorf("unexpected text events: %v", textEvents)
	}

	// Check final message
	if finalMessage == nil {
		t.Fatal("expected final message")
	}
	if finalMessage.ID != "msg_test" {
		t.Errorf("ID: got %s", finalMessage.ID)
	}
	if len(finalMessage.Content) != 1 {
		t.Errorf("Content: got %d blocks", len(finalMessage.Content))
	}
	if finalMessage.Content[0].Text != "Hello, world!" {
		t.Errorf("Text: got %s", finalMessage.Content[0].Text)
	}
}

func TestRunAgentSync(t *testing.T) {
	sseResponse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_sync","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":"","usage":{"input_tokens":5,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Sync response"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

event: message_stop
data: {}
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	client := api.NewClient("test-key", api.WithBaseURL(server.URL))

	ctx := context.Background()
	text, err := RunAgentSync(ctx, "hello", AgentOptions{
		Client: client,
	})

	if err != nil {
		t.Fatalf("RunAgentSync: %v", err)
	}
	if text != "Sync response" {
		t.Errorf("text: got %q", text)
	}
}

func TestRunAgentWithThinking(t *testing.T) {
	sseResponse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_think","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":"","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Thinking..."}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}

event: message_stop
data: {}
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	client := api.NewClient("test-key", api.WithBaseURL(server.URL))

	ctx := context.Background()
	text, err := RunAgentSync(ctx, "hello", AgentOptions{
		Client: client,
		Model:  "claude-sonnet-4-6",
	})

	if err != nil {
		t.Fatalf("RunAgentSync: %v", err)
	}
	if text != "Answer" {
		t.Errorf("text: got %q", text)
	}
}

func TestRunAgentError(t *testing.T) {
	sseResponse := `event: error
data: {"type":"authentication_error","message":"Invalid API key"}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	client := api.NewClient("test-key", api.WithBaseURL(server.URL))

	ctx := context.Background()
	_, err := RunAgentSync(ctx, "hello", AgentOptions{
		Client: client,
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "authentication_error") {
		t.Errorf("error: got %v", err)
	}
}

func TestIsThinkingModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"claude-sonnet-4-6", true},
		{"claude-sonnet-4-5-20250514", true},
		{"claude-opus-4-6", true},
		{"claude-opus-4-5-20250514", true},
		{"claude-3-5-sonnet-20241022", false},
		{"claude-3-opus-20240229", false},
		{"claude-3-5-haiku-20241022", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := isThinkingModel(tt.model)
			if got != tt.want {
				t.Errorf("isThinkingModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestRunAgentWithOptions(t *testing.T) {
	sseResponse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_opts","type":"message","role":"assistant","content":[],"model":"claude-opus-4-6","stop_reason":"","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

event: message_stop
data: {}
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	client := api.NewClient("test-key", api.WithBaseURL(server.URL))

	ctx := context.Background()
	text, err := RunAgentSync(ctx, "hello", AgentOptions{
		Client:       client,
		Model:        "claude-opus-4-6",
		MaxTokens:    8192,
		SystemPrompt: []api.SystemBlock{{Type: "text", Text: "You are helpful"}},
	})

	if err != nil {
		t.Fatalf("RunAgentSync: %v", err)
	}
	if text != "OK" {
		t.Errorf("text: got %q", text)
	}
}
