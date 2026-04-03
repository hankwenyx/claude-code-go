package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/mcp"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

// ---- stdio client tests ----

// writeMCPServer writes a minimal MCP server script and returns its path.
// The server responds to initialize, notifications/initialized, tools/list, and tools/call.
func writeMCPServer(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("skip stdio MCP server test on Windows")
	}
	script := `#!/usr/bin/env python3
import sys, json

def send(obj):
    print(json.dumps(obj), flush=True)

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    method = req.get("method", "")
    rid = req.get("id")

    if method == "initialize":
        send({"jsonrpc":"2.0","id":rid,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"test","version":"0.1"}}})
    elif method == "notifications/initialized":
        pass  # notification, no response
    elif method == "tools/list":
        send({"jsonrpc":"2.0","id":rid,"result":{"tools":[{"name":"echo","description":"echo input","inputSchema":{"type":"object","properties":{"text":{"type":"string"}}}}]}})
    elif method == "tools/call":
        params = req.get("params", {})
        args = params.get("arguments", {})
        text = args.get("text", "")
        send({"jsonrpc":"2.0","id":rid,"result":{"content":[{"type":"text","text":text}],"isError":False}})
    else:
        if rid is not None:
            send({"jsonrpc":"2.0","id":rid,"error":{"code":-32601,"message":"method not found"}})
`
	dir := t.TempDir()
	path := filepath.Join(dir, "server.py")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	// Check python3 is available
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found; skipping stdio MCP test")
	}
	return path
}

func TestStdioClientConnectAndCall(t *testing.T) {
	script := writeMCPServer(t)
	cfg := mcp.ServerConfig{Command: "python3", Args: []string{script}}
	c := mcp.NewStdioClient("test", cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer c.Close()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if len(c.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(c.Tools))
	}
	if c.Tools[0].Name != "echo" {
		t.Errorf("expected tool 'echo', got %q", c.Tools[0].Name)
	}

	args, _ := json.Marshal(map[string]string{"text": "hello"})
	content, isErr, err := c.CallTool(ctx, "echo", args)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if isErr {
		t.Error("expected isError=false")
	}
	if content != "hello" {
		t.Errorf("expected 'hello', got %q", content)
	}
}

// ---- mcpTool adapter tests (no subprocess needed) ----

type fakeCaller struct {
	content string
	isError bool
	err     error
}

func (f *fakeCaller) CallTool(_ context.Context, _ string, _ json.RawMessage) (string, bool, error) {
	return f.content, f.isError, f.err
}

func toolForTest(serverName, toolName string, fc mcp.Caller) tools.Tool {
	def := mcp.ToolDef{
		Name:        toolName,
		Description: "test tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
	return mcp.NewToolForTest(serverName, def, fc)
}

func TestMCPToolName(t *testing.T) {
	fc := &fakeCaller{content: "ok"}
	tool := toolForTest("myserver", "do_thing", fc)
	if tool.Name() != "mcp__myserver__do_thing" {
		t.Errorf("unexpected name: %q", tool.Name())
	}
}

func TestMCPToolCall(t *testing.T) {
	fc := &fakeCaller{content: "result text"}
	tool := toolForTest("s", "t", fc)
	res, err := tool.Call(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "result text" {
		t.Errorf("got %q", res.Content)
	}
}

func TestMCPToolPermissions(t *testing.T) {
	fc := &fakeCaller{}
	tool := toolForTest("fs", "read_file", fc)

	cases := []struct {
		rules    tools.PermissionRules
		behavior string
	}{
		{tools.PermissionRules{Deny: []string{"mcp__fs__read_file"}}, "deny"},
		{tools.PermissionRules{Allow: []string{"mcp__fs"}}, "allow"},
		{tools.PermissionRules{Allow: []string{"mcp__fs__*"}}, "allow"},
		{tools.PermissionRules{Allow: []string{"mcp__fs__read*"}}, "allow"},
		{tools.PermissionRules{Ask: []string{"mcp__fs__read_file"}}, "ask"},
		{tools.PermissionRules{}, "ask"}, // default
	}
	for _, tc := range cases {
		dec := tool.CheckPermissions(nil, "auto", tc.rules)
		if dec.Behavior != tc.behavior {
			t.Errorf("rules=%+v: want %q, got %q", tc.rules, tc.behavior, dec.Behavior)
		}
	}
}

// ---- SSE client test ----

func TestSSEClientConnectAndCall(t *testing.T) {
	// Build a minimal fake MCP SSE server.
	type pendingReq struct {
		id     int64
		method string
		params json.RawMessage
	}

	// SSE message helper
	sseMsg := func(w http.ResponseWriter, event, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	// A channel to carry client messages to the SSE response writer.
	type serverResponse struct {
		id     *int64
		result json.RawMessage
	}
	respCh := make(chan serverResponse, 10)

	var sseW http.ResponseWriter
	sseReady := make(chan struct{})

	mux := http.NewServeMux()

	// GET /sse — opens the event stream
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		sseW = w
		// Send endpoint event
		sseMsg(w, "endpoint", "/message")
		close(sseReady)
		// Stream responses
		for resp := range respCh {
			raw, _ := json.Marshal(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      resp.id,
				"result":  resp.result,
			})
			sseMsg(w, "message", string(raw))
		}
	})

	// POST /message — receives JSON-RPC requests
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		w.WriteHeader(http.StatusAccepted)

		if req.ID == nil {
			return // notification
		}

		var result json.RawMessage
		switch req.Method {
		case "initialize":
			result, _ = json.Marshal(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "test-sse", "version": "0.1"},
			})
		case "tools/list":
			result, _ = json.Marshal(map[string]interface{}{
				"tools": []map[string]interface{}{
					{"name": "ping", "description": "ping tool", "inputSchema": map[string]interface{}{"type": "object"}},
				},
			})
		case "tools/call":
			result, _ = json.Marshal(map[string]interface{}{
				"content": []map[string]interface{}{{"type": "text", "text": "pong"}},
				"isError": false,
			})
		}
		respCh <- serverResponse{id: req.ID, result: result}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer close(respCh)

	// Wait for SSE handler to be ready (it starts when the client connects).
	cfg := mcp.ServerConfig{URL: srv.URL + "/sse"}
	c := mcp.NewSSEClient("test-sse", cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("SSEClient.Connect: %v", err)
	}
	_ = sseW // suppress unused warning

	if len(c.Tools) != 1 || c.Tools[0].Name != "ping" {
		t.Fatalf("expected tool 'ping', got %+v", c.Tools)
	}

	content, isErr, err := c.CallTool(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if isErr || content != "pong" {
		t.Errorf("got isErr=%v content=%q", isErr, content)
	}
}

func TestMCPToolIsReadOnly(t *testing.T) {
	fc := &fakeCaller{}
	tool := toolForTest("s", "t", fc)
	if tool.IsReadOnly() {
		t.Error("MCP tools should not be read-only by default")
	}
}

func TestMCPToolInputSchema_Nil(t *testing.T) {
	// ToolDef with nil InputSchema → should return default schema
	def := mcp.ToolDef{
		Name:        "no_schema",
		Description: "test",
		InputSchema: nil,
	}
	tool := mcp.NewToolForTest("s", def, &fakeCaller{})
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("InputSchema should return default, not empty")
	}
}

func TestMCPToolCallError(t *testing.T) {
	fc := &fakeCaller{err: fmt.Errorf("connection lost")}
	tool := toolForTest("s", "t", fc)
	res, err := tool.Call(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !res.IsError {
		t.Error("expected IsError=true on error")
	}
}

func TestMCPToolCallIsError(t *testing.T) {
	fc := &fakeCaller{content: "oops", isError: true}
	tool := toolForTest("s", "t", fc)
	res, err := tool.Call(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
	if res.Content != "oops" {
		t.Errorf("content: %q", res.Content)
	}
}

func TestMatchMCPRule_NoMatch(t *testing.T) {
	fc := &fakeCaller{}
	tool := toolForTest("fs", "read_file", fc)
	dec := tool.CheckPermissions(nil, "auto", tools.PermissionRules{
		Allow: []string{"mcp__git__commit"}, // different server
	})
	if dec.Behavior != "ask" {
		t.Errorf("non-matching allow should fall through to ask, got %q", dec.Behavior)
	}
}

func TestStdioClientConnectBadCommand(t *testing.T) {
	cfg := mcp.ServerConfig{Command: "this-command-does-not-exist-xyz"}
	c := mcp.NewStdioClient("bad", cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.Connect(ctx)
	if err == nil {
		t.Error("expected error for non-existent command")
	}
}

func TestStdioClientEmptyCommand(t *testing.T) {
	cfg := mcp.ServerConfig{Command: ""}
	c := mcp.NewStdioClient("empty", cfg)
	err := c.Connect(context.Background())
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestSettingsMCPServersMerge(t *testing.T) {
	dir := t.TempDir()
	userSettings := filepath.Join(dir, "user.json")
	projectSettings := filepath.Join(dir, "project.json")

	user := `{"mcpServers":{"fs":{"command":"npx","args":["-y","server-fs"]}}}`
	project := `{"mcpServers":{"git":{"command":"npx","args":["-y","server-git"]}}}`

	_ = os.WriteFile(userSettings, []byte(user), 0644)
	_ = os.WriteFile(projectSettings, []byte(project), 0644)

	// Directly test merge via exported config API isn't ideal since LoadSettings
	// uses fixed paths. Instead verify the struct parses correctly.
	var s1, s2 struct {
		MCPServers map[string]mcp.ServerConfig `json:"mcpServers"`
	}
	_ = json.Unmarshal([]byte(user), &s1)
	_ = json.Unmarshal([]byte(project), &s2)

	merged := make(map[string]mcp.ServerConfig)
	for k, v := range s1.MCPServers {
		merged[k] = v
	}
	for k, v := range s2.MCPServers {
		merged[k] = v
	}

	if _, ok := merged["fs"]; !ok {
		t.Error("missing 'fs' server")
	}
	if _, ok := merged["git"]; !ok {
		t.Error("missing 'git' server")
	}
	_ = strings.TrimSpace("") // avoid import cycle warning
}

// ---- Manager tests ----

func TestManager_EmptyConnect(t *testing.T) {
	m := mcp.NewManager()
	errs := m.Connect(context.Background(), nil)
	if len(errs) != 0 {
		t.Errorf("expected no errors for empty config, got %v", errs)
	}
}

func TestManager_RegisterAll_Empty(t *testing.T) {
	m := mcp.NewManager()
	reg := tools.NewRegistry()
	m.RegisterAll(reg) // should not panic with no clients
	if len(reg.All()) != 0 {
		t.Errorf("expected empty registry, got %v", reg.All())
	}
}

func TestManager_Close_Empty(t *testing.T) {
	m := mcp.NewManager()
	m.Close() // should not panic
}

func TestManager_ConnectFails_BadCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on Windows")
	}
	m := mcp.NewManager()
	errs := m.Connect(context.Background(), map[string]mcp.ServerConfig{
		"bad": {Command: "/nonexistent/binary/xyz"},
	})
	if len(errs) == 0 {
		t.Error("expected error for bad command")
	}
}

func TestManager_ConnectStdio_AndRegister(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on Windows")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}

	script := writeMCPServer(t)
	m := mcp.NewManager()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer m.Close()

	errs := m.Connect(ctx, map[string]mcp.ServerConfig{
		"testserver": {Command: "python3", Args: []string{script}},
	})
	if len(errs) != 0 {
		t.Fatalf("Connect errors: %v", errs)
	}

	reg := tools.NewRegistry()
	m.RegisterAll(reg)

	allTools := reg.All()
	var names []string
	for _, t := range allTools {
		names = append(names, t.Name())
	}
	if len(names) == 0 {
		t.Fatal("expected at least one tool registered")
	}
	// Tool should be namespaced
	found := false
	for _, n := range names {
		if strings.HasPrefix(n, "mcp__testserver__") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected mcp__testserver__ prefix in names: %v", names)
	}
}

func TestManager_ConnectSSE_AndRegister(t *testing.T) {
	// Inline minimal SSE server
	type serverResponse struct {
		id     *int64
		result json.RawMessage
	}
	respCh := make(chan serverResponse, 10)
	sseReady := make(chan struct{})
	var sseWMu sync.Mutex
	var sseWRef http.ResponseWriter

	sseMsg := func(w http.ResponseWriter, event, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseWMu.Lock()
		sseWRef = w
		sseWMu.Unlock()
		sseMsg(w, "endpoint", "/message")
		close(sseReady)
		for resp := range respCh {
			raw, _ := json.Marshal(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      resp.id,
				"result":  resp.result,
			})
			sseMsg(w, "message", string(raw))
		}
		_ = sseWRef
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		w.WriteHeader(http.StatusAccepted)
		if req.ID == nil {
			return
		}
		var result json.RawMessage
		switch req.Method {
		case "initialize":
			result, _ = json.Marshal(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "sse-mgr", "version": "0.1"},
			})
		case "tools/list":
			result, _ = json.Marshal(map[string]interface{}{
				"tools": []map[string]interface{}{
					{"name": "greet", "description": "greet", "inputSchema": map[string]interface{}{"type": "object"}},
				},
			})
		}
		respCh <- serverResponse{id: req.ID, result: result}
	})

	ts := httptest.NewServer(mux)
	defer func() {
		close(respCh)
		ts.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m := mcp.NewManager()
	errs := m.Connect(ctx, map[string]mcp.ServerConfig{
		"sseserver": {URL: ts.URL + "/sse"},
	})
	if len(errs) != 0 {
		t.Fatalf("Connect errors: %v", errs)
	}

	reg := tools.NewRegistry()
	m.RegisterAll(reg)

	allTools := reg.All()
	var names []string
	for _, t := range allTools {
		names = append(names, t.Name())
	}
	found := false
	for _, n := range names {
		if strings.HasPrefix(n, "mcp__sseserver__") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected mcp__sseserver__ prefix in names: %v", names)
	}
}
