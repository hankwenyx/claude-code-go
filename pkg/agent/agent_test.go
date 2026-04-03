package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/api"
	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/permissions"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
	"github.com/hankwenyx/claude-code-go/pkg/tools/task"
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

// mockTool is a simple tool for testing that returns a fixed result.
type mockTool struct {
	name   string
	result string
	called atomic.Bool
}

func (m *mockTool) Name() string                 { return m.name }
func (m *mockTool) Description() string          { return "mock tool" }
func (m *mockTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (m *mockTool) IsReadOnly() bool             { return true }
func (m *mockTool) Call(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	m.called.Store(true)
	return tools.ToolResult{Content: m.result}, nil
}
func (m *mockTool) CheckPermissions(_ json.RawMessage, _ string, _ tools.PermissionRules) tools.PermissionDecision {
	return tools.PermissionDecision{Behavior: "allow"}
}

// TestRunAgentToolUse verifies the full tool_use → tool_result → text loop:
// Turn 1: model emits a tool_use block → agent calls the tool
// Turn 2: server returns the final text reply
func TestRunAgentToolUse(t *testing.T) {
	// Turn 1: assistant wants to call "MockTool"
	turn1 := `event: message_start
data: {"type":"message_start","message":{"id":"msg_t1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":"","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"MockTool"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}

event: message_stop
data: {}
`
	// Turn 2: assistant returns final answer after seeing the tool result
	turn2 := `event: message_start
data: {"type":"message_start","message":{"id":"msg_t2","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":"","usage":{"input_tokens":20,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"tool result was: mock-output"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}

event: message_stop
data: {}
`
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := requestCount.Add(1)
		if n == 1 {
			w.Write([]byte(turn1))
		} else {
			w.Write([]byte(turn2))
		}
	}))
	defer server.Close()

	tool := &mockTool{name: "MockTool", result: "mock-output"}
	reg := tools.NewRegistry()
	reg.Register(tool)

	client := api.NewClient("test-key", api.WithBaseURL(server.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var toolUseEvents []string
	var toolResultEvents []string
	var textParts []string

	events := RunAgent(ctx, "use the tool", AgentOptions{
		Client:   client,
		Registry: reg,
	})
	for ev := range events {
		switch ev.Type {
		case EventToolUse:
			toolUseEvents = append(toolUseEvents, ev.ToolCall.Name)
		case EventToolResult:
			toolResultEvents = append(toolResultEvents, ev.ToolResult.Content)
		case EventText:
			textParts = append(textParts, ev.Text)
		case EventError:
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}

	// Tool must have been called
	if !tool.called.Load() {
		t.Error("mockTool.Call was never invoked")
	}
	// EventToolUse emitted with correct name
	if len(toolUseEvents) != 1 || toolUseEvents[0] != "MockTool" {
		t.Errorf("toolUseEvents: %v", toolUseEvents)
	}
	// EventToolResult carries the mock output
	if len(toolResultEvents) != 1 || toolResultEvents[0] != "mock-output" {
		t.Errorf("toolResultEvents: %v", toolResultEvents)
	}
	// Final text contains the tool result echoed back
	finalText := strings.Join(textParts, "")
	if !strings.Contains(finalText, "mock-output") {
		t.Errorf("final text %q does not mention tool result", finalText)
	}
	// Two HTTP requests: one per turn
	if n := requestCount.Load(); n != 2 {
		t.Errorf("expected 2 HTTP requests, got %d", n)
	}
}

// ---- Unit tests for unexported helpers ----

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"429 in message", fmt.Errorf("HTTP 429 too many requests"), true},
		{"529 in message", fmt.Errorf("server returned 529"), true},
		{"503", fmt.Errorf("503 service unavailable"), true},
		{"502", fmt.Errorf("bad gateway 502"), true},
		{"504", fmt.Errorf("gateway timeout 504"), true},
		{"rate limit", fmt.Errorf("rate limit exceeded"), true},
		{"ratelimit", fmt.Errorf("ratelimit hit"), true},
		{"rate_limit", fmt.Errorf("rate_limit exceeded"), true},
		{"too many request", fmt.Errorf("too many request from client"), true},
		{"quota", fmt.Errorf("quota exceeded"), true},
		{"qps", fmt.Errorf("qps exceeded"), true},
		{"overloaded", fmt.Errorf("server overloaded"), true},
		{"overload", fmt.Errorf("model overload"), true},
		{"timeout", fmt.Errorf("request timeout"), true},
		{"timed out", fmt.Errorf("connection timed out"), true},
		{"deadline exceeded", fmt.Errorf("context deadline exceeded"), true},
		{"connection reset", fmt.Errorf("connection reset by peer"), true},
		{"connection refused", fmt.Errorf("connection refused"), true},
		{"eof", fmt.Errorf("unexpected EOF"), true},
		{"broken pipe", fmt.Errorf("broken pipe"), true},
		{"i/o timeout", fmt.Errorf("read: i/o timeout"), true},
		{"超限", fmt.Errorf("超限错误"), true},
		{"限流", fmt.Errorf("限流触发"), true},
		{"ordinary error", fmt.Errorf("invalid API key"), false},
		{"not found", fmt.Errorf("404 not found"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryable(tc.err); got != tc.want {
				t.Errorf("isRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type statusCodeErr struct {
	code int
}

func (e statusCodeErr) Error() string   { return fmt.Sprintf("http %d", e.code) }
func (e statusCodeErr) StatusCode() int { return e.code }

func TestIsRetryable_StatusCoder(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{429, true},
		{503, true},
		{502, true},
		{504, true},
		{529, true},
		{401, false},
		{404, false},
		{200, false},
	}
	for _, tc := range cases {
		if got := isRetryable(statusCodeErr{tc.code}); got != tc.want {
			t.Errorf("isRetryable(status=%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestRetryBackoff(t *testing.T) {
	for attempt := 1; attempt <= 6; attempt++ {
		d := retryBackoff(attempt)
		if d < time.Second {
			t.Errorf("attempt %d: backoff %v < 1s minimum", attempt, d)
		}
		if d > 40*time.Second { // 30s max + 20% jitter headroom
			t.Errorf("attempt %d: backoff %v exceeds reasonable max", attempt, d)
		}
	}
	// Higher attempt should cap near maxWait (30s)
	d := retryBackoff(10)
	if d < 20*time.Second {
		t.Errorf("attempt 10: expected near-maxWait, got %v", d)
	}
}

func TestPersistToolResult_WritesToDisk(t *testing.T) {
	large := strings.Repeat("z", 10000)
	opts := AgentOptions{
		CWD:       t.TempDir(),
		SessionID: "sess-abc",
	}
	out := persistToolResult("toolu_big", large, opts)
	if !strings.Contains(out, "persisted-output") {
		t.Errorf("expected persisted-output tag: %q", out)
	}
	if !strings.Contains(out, "10000") {
		t.Errorf("expected byte count in output: %q", out)
	}
}

func TestPersistToolResult_DiskFallback(t *testing.T) {
	// Use an invalid CWD so disk write fails — falls back to inline truncation
	opts := AgentOptions{
		CWD:       "/nonexistent/xyz/that/cannot/be/created",
		SessionID: "s",
	}
	content := strings.Repeat("y", 5000)
	out := persistToolResult("t1", content, opts)
	if !strings.Contains(out, "persisted-output") {
		t.Errorf("expected persisted-output tag: %q", out)
	}
}

func TestCallTool_NotFound(t *testing.T) {
	events := make(chan AgentEvent, 10)
	tu := api.ContentBlock{
		Type:  "tool_use",
		ID:    "toolu_nf",
		Name:  "NoSuchTool",
		Input: json.RawMessage(`{}`),
	}
	opts := AgentOptions{Registry: tools.NewRegistry()}
	result := callTool(context.Background(), tu, opts, events)
	close(events)

	if !result.IsError {
		t.Error("expected IsError=true")
	}
	if !strings.Contains(result.Content, "tool not found") {
		t.Errorf("content: %q", result.Content)
	}
	var types []EventType
	for ev := range events {
		types = append(types, ev.Type)
	}
	if len(types) != 2 {
		t.Errorf("expected 2 events (EventToolUse+EventToolResult), got %v", types)
	}
}

func TestCallTool_PermissionDenied(t *testing.T) {
	events := make(chan AgentEvent, 10)

	tool := &mockTool{name: "Restricted"}
	reg := tools.NewRegistry()
	reg.Register(tool)

	// Checker with a deny rule for "Restricted" tool + non-interactive so ask→deny
	checker := &permissions.Checker{
		Mode:           permissions.ModeDefault,
		NonInteractive: true,
		Rules:          config.PermissionRules{Deny: []string{"Restricted"}},
	}

	tu := api.ContentBlock{
		Type:  "tool_use",
		ID:    "toolu_deny",
		Name:  "Restricted",
		Input: json.RawMessage(`{}`),
	}
	opts := AgentOptions{Registry: reg, PermChecker: checker}
	result := callTool(context.Background(), tu, opts, events)
	close(events)

	if !result.IsError {
		t.Error("expected IsError=true for denied tool")
	}
	if !strings.Contains(result.Content, "permission denied") {
		t.Errorf("content: %q", result.Content)
	}
	if tool.called.Load() {
		t.Error("tool should not have been called when permission denied")
	}
}

func TestRunAgent_MaxTurns(t *testing.T) {
	toolUseResp := `event: message_start
data: {"type":"message_start","message":{"id":"msg_mt","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":"","usage":{"input_tokens":5,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"MockTool"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}

event: message_stop
data: {}
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(toolUseResp))
	}))
	defer server.Close()

	tool := &mockTool{name: "MockTool", result: "r"}
	reg := tools.NewRegistry()
	reg.Register(tool)

	client := api.NewClient("test-key", api.WithBaseURL(server.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := RunAgentSync(ctx, "loop", AgentOptions{
		Client:   client,
		Registry: reg,
		MaxTurns: 2,
	})
	if err == nil {
		t.Fatal("expected max turns error")
	}
	if !strings.Contains(err.Error(), "max turns") {
		t.Errorf("expected 'max turns' error, got: %v", err)
	}
}

func TestRunAgent_TaskManagerNotifications(t *testing.T) {
	sseResp := `event: message_start
data: {"type":"message_start","message":{"id":"msg_n","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":"","usage":{"input_tokens":5,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

event: message_stop
data: {}
`
	var capturedBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if msgs, ok := body["messages"]; ok {
			capturedBodies = append(capturedBodies, string(msgs))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseResp))
	}))
	defer server.Close()

	client := api.NewClient("test-key", api.WithBaseURL(server.URL))

	mgr := task.NewManager(4)
	done := make(chan struct{})
	runner := func(_ context.Context, _ string) (string, error) {
		<-done
		return "background done", nil
	}
	mgr.Dispatch(context.Background(), "bg-task", "do something", runner)
	close(done)
	time.Sleep(80 * time.Millisecond) // let goroutine finish

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for range RunAgent(ctx, "check tasks", AgentOptions{
		Client:      client,
		TaskManager: mgr,
	}) {
	}

	if len(capturedBodies) == 0 {
		t.Fatal("no requests captured")
	}
	found := false
	for _, b := range capturedBodies {
		if strings.Contains(b, "task-notification") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("task-notification not found in any request body; bodies: %v", capturedBodies)
	}
}

// TestRunAgentMultiTurnHistory verifies that opts.Messages is forwarded to the
// second RunAgent call, so conversation context is preserved across TUI turns.
func TestRunAgentMultiTurnHistory(t *testing.T) {
	sseResp := `event: message_start
data: {"type":"message_start","message":{"id":"msg_h","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":"","usage":{"input_tokens":5,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"reply"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

event: message_stop
data: {}
`
	var lastBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastBody, _ = json.Marshal(nil) // reset
		if err := json.NewDecoder(r.Body).Decode(&lastBody); err == nil {
			_ = lastBody
		}
		// Re-read properly
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseResp))
	}))
	defer server.Close()

	client := api.NewClient("test-key", api.WithBaseURL(server.URL))
	ctx := context.Background()

	// Turn 1
	var history []api.APIMessage
	for ev := range RunAgent(ctx, "first message", AgentOptions{Client: client}) {
		if ev.Type == EventMessage {
			history = ev.Messages
		}
	}
	if len(history) == 0 {
		t.Fatal("EventMessage.Messages is empty after turn 1")
	}

	// Turn 2 — pass history back; server receives both messages
	var seenMessages []api.APIMessage
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []api.APIMessage `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		seenMessages = req.Messages
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseResp))
	}))
	defer server2.Close()

	client2 := api.NewClient("test-key", api.WithBaseURL(server2.URL))
	for range RunAgent(ctx, "second message", AgentOptions{
		Client:   client2,
		Messages: history,
	}) {
	}

	// The second request should contain at least the prior user+assistant turn
	// plus the new user message → at least 3 messages total
	if len(seenMessages) < 3 {
		t.Errorf("expected ≥3 messages in turn-2 request, got %d: %+v", len(seenMessages), seenMessages)
	}
}
