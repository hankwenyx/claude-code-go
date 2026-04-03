package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// SSEClient connects to an MCP server over HTTP + Server-Sent Events.
//
// Protocol:
//  1. Client opens a persistent GET to cfg.URL → receives SSE stream.
//  2. Server sends an "endpoint" event containing the POST URL.
//  3. Client sends JSON-RPC messages via HTTP POST to that endpoint.
//  4. Server delivers responses as "message" events on the SSE stream.
type SSEClient struct {
	serverName string
	cfg        ServerConfig
	httpClient *http.Client

	postURL string // received from "endpoint" SSE event

	mu      sync.Mutex
	pending map[int64]chan jsonRPCResponse
	idGen   atomic.Int64

	Tools []ToolDef
}

// NewSSEClient creates a client (does not connect yet).
func NewSSEClient(serverName string, cfg ServerConfig) *SSEClient {
	return &SSEClient{
		serverName: serverName,
		cfg:        cfg,
		httpClient: &http.Client{},
		pending:    make(map[int64]chan jsonRPCResponse),
	}
}

// Connect opens the SSE stream, waits for the endpoint event, then initialises.
func (c *SSEClient) Connect(ctx context.Context) error {
	if c.cfg.URL == "" {
		return fmt.Errorf("mcp sse %q: url is empty", c.serverName)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("mcp sse %q: %w", c.serverName, err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp sse %q: GET: %w", c.serverName, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return fmt.Errorf("mcp sse %q: GET status %d", c.serverName, resp.StatusCode)
	}

	// Wait for the "endpoint" event before starting full reader goroutine.
	endpointCh := make(chan string, 1)
	go c.readSSE(resp.Body, endpointCh)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case ep := <-endpointCh:
		c.postURL = ep
	}

	// Resolve relative endpoint URLs against the base URL.
	if !strings.HasPrefix(c.postURL, "http") {
		base := c.cfg.URL
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[:idx]
		}
		c.postURL = base + "/" + strings.TrimPrefix(c.postURL, "/")
	}

	// MCP initialize handshake.
	params, _ := json.Marshal(initializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    struct{}{},
		ClientInfo:      clientInfo{Name: "claude-code-go", Version: "0.1.0"},
	})
	if _, err := c.request(ctx, "initialize", params); err != nil {
		return fmt.Errorf("mcp sse %q: initialize: %w", c.serverName, err)
	}
	_ = c.notify("notifications/initialized")

	tools, err := c.listTools(ctx)
	if err != nil {
		return fmt.Errorf("mcp sse %q: tools/list: %w", c.serverName, err)
	}
	c.Tools = tools
	return nil
}

// CallTool executes a tool and returns (content, isError, err).
func (c *SSEClient) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	params, _ := json.Marshal(callToolParams{Name: name, Arguments: args})
	raw, err := c.request(ctx, "tools/call", params)
	if err != nil {
		return "", true, err
	}
	var result callToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", true, fmt.Errorf("tools/call unmarshal: %w", err)
	}
	var out string
	for _, b := range result.Content {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out, result.IsError, nil
}

// --- internal ---

func (c *SSEClient) listTools(ctx context.Context) ([]ToolDef, error) {
	raw, err := c.request(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var result listToolsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (c *SSEClient) request(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	id := c.idGen.Add(1)
	ch := make(chan jsonRPCResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	req := jsonRPCRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}
	if err := c.post(ctx, req); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *SSEClient) notify(method string) error {
	return c.post(context.Background(), jsonRPCRequest{JSONRPC: "2.0", Method: method})
}

func (c *SSEClient) post(ctx context.Context, req jsonRPCRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.postURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(hreq)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("post status %d", resp.StatusCode)
	}
	return nil
}

// readSSE reads the SSE stream and dispatches events.
// The first "endpoint" event URL is sent to endpointCh (once), then closed.
func (c *SSEClient) readSSE(body io.ReadCloser, endpointCh chan<- string) {
	defer body.Close()

	scanner := bufio.NewScanner(body)
	var eventType, dataLine string
	endpointSent := false

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		case line == "":
			// Blank line = dispatch event
			switch eventType {
			case "endpoint":
				if !endpointSent {
					endpointSent = true
					endpointCh <- dataLine
					close(endpointCh)
				}
			case "message":
				var resp jsonRPCResponse
				if err := json.Unmarshal([]byte(dataLine), &resp); err == nil && resp.ID != nil {
					c.mu.Lock()
					ch, ok := c.pending[*resp.ID]
					if ok {
						delete(c.pending, *resp.ID)
					}
					c.mu.Unlock()
					if ok {
						ch <- resp
					}
				}
			}
			eventType = ""
			dataLine = ""
		}
	}
}
