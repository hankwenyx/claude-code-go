package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// StdioClient connects to an MCP server over stdin/stdout using line-delimited JSON-RPC 2.0.
type StdioClient struct {
	serverName string
	cfg        ServerConfig

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner

	mu      sync.Mutex
	pending map[int64]chan jsonRPCResponse
	idGen   atomic.Int64

	Tools []ToolDef // populated after Connect+ListTools
}

// NewStdioClient creates a client (does not start the process yet).
func NewStdioClient(serverName string, cfg ServerConfig) *StdioClient {
	return &StdioClient{
		serverName: serverName,
		cfg:        cfg,
		pending:    make(map[int64]chan jsonRPCResponse),
	}
}

// Connect starts the MCP server subprocess, performs the initialize handshake,
// and fetches the tool list.
func (c *StdioClient) Connect(ctx context.Context) error {
	if c.cfg.Command == "" {
		return fmt.Errorf("mcp server %q: command is empty", c.serverName)
	}

	cmd := exec.CommandContext(ctx, c.cfg.Command, c.cfg.Args...)
	// Inject extra env vars on top of the current process environment.
	if len(c.cfg.Env) > 0 {
		cmd.Env = cmd.Environ()
		for k, v := range c.cfg.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp %q: stdin pipe: %w", c.serverName, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp %q: stdout pipe: %w", c.serverName, err)
	}
	// Discard stderr so it doesn't pollute the TUI.
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcp %q: start: %w", c.serverName, err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewScanner(stdout)

	// Start the response-dispatch goroutine.
	go c.readLoop()

	// MCP initialize handshake.
	params, _ := json.Marshal(initializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    struct{}{},
		ClientInfo:      clientInfo{Name: "claude-code-go", Version: "0.1.0"},
	})
	if _, err := c.request(ctx, "initialize", params); err != nil {
		return fmt.Errorf("mcp %q: initialize: %w", c.serverName, err)
	}
	// Send notifications/initialized (no ID → notification, ignore response).
	_ = c.notify("notifications/initialized")

	// Fetch tool list.
	tools, err := c.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("mcp %q: tools/list: %w", c.serverName, err)
	}
	c.Tools = tools
	return nil
}

// ListTools calls tools/list and returns all available tools.
func (c *StdioClient) ListTools(ctx context.Context) ([]ToolDef, error) {
	raw, err := c.request(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var result listToolsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("tools/list unmarshal: %w", err)
	}
	return result.Tools, nil
}

// CallTool executes an MCP tool and returns the content blocks as a string.
func (c *StdioClient) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	params, _ := json.Marshal(callToolParams{Name: name, Arguments: args})
	raw, err := c.request(ctx, "tools/call", params)
	if err != nil {
		return "", true, err
	}
	var result callToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", true, fmt.Errorf("tools/call unmarshal: %w", err)
	}
	// Concatenate all text content blocks.
	var out string
	for _, b := range result.Content {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out, result.IsError, nil
}

// Close shuts down the MCP server process.
func (c *StdioClient) Close() {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil {
		_ = c.cmd.Wait()
	}
}

// --- internal helpers ---

// request sends a JSON-RPC request and blocks until the response arrives.
func (c *StdioClient) request(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	id := c.idGen.Add(1)
	ch := make(chan jsonRPCResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  params,
	}
	if err := c.sendRaw(req); err != nil {
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

// notify sends a JSON-RPC notification (no ID, no response expected).
func (c *StdioClient) notify(method string) error {
	return c.sendRaw(jsonRPCRequest{JSONRPC: "2.0", Method: method})
}

// sendRaw serialises req and writes it as a single newline-terminated line.
func (c *StdioClient) sendRaw(req jsonRPCRequest) error {
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	c.mu.Lock()
	_, err = c.stdin.Write(line)
	c.mu.Unlock()
	return err
}

// readLoop runs in a goroutine and dispatches incoming responses to pending channels.
func (c *StdioClient) readLoop() {
	for c.stdout.Scan() {
		line := c.stdout.Bytes()
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // malformed line — skip
		}
		if resp.ID == nil {
			continue // notification from server — ignore for now
		}
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
