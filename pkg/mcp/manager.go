package mcp

import (
	"context"
	"fmt"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

// Manager owns and lifecycle-manages a set of MCP server connections.
type Manager struct {
	clients []managedClient
}

type managedClient struct {
	name     string
	closer   func()
	toolDefs []ToolDef
	caller   caller
}

// NewManager creates an empty Manager.
func NewManager() *Manager {
	return &Manager{}
}

// Connect starts all configured MCP servers and fetches their tool lists.
// Servers that fail to start are logged and skipped (best-effort).
func (m *Manager) Connect(ctx context.Context, configs map[string]ServerConfig) []error {
	var errs []error
	for name, cfg := range configs {
		mc, err := connectOne(ctx, name, cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp %q: %w", name, err))
			continue
		}
		m.clients = append(m.clients, mc)
	}
	return errs
}

// RegisterAll registers all connected MCP tools into the given registry.
func (m *Manager) RegisterAll(registry *tools.Registry) {
	for _, mc := range m.clients {
		for _, def := range mc.toolDefs {
			registry.Register(newMCPTool(mc.name, def, mc.caller))
		}
	}
}

// Close shuts down all managed MCP server processes.
func (m *Manager) Close() {
	for _, mc := range m.clients {
		if mc.closer != nil {
			mc.closer()
		}
	}
}

// connectOne creates and connects a single MCP client.
func connectOne(ctx context.Context, name string, cfg ServerConfig) (managedClient, error) {
	if cfg.URL != "" {
		// SSE transport
		c := NewSSEClient(name, cfg)
		if err := c.Connect(ctx); err != nil {
			return managedClient{}, err
		}
		return managedClient{
			name:     name,
			closer:   func() {},
			toolDefs: c.Tools,
			caller:   c,
		}, nil
	}

	// Stdio transport
	c := NewStdioClient(name, cfg)
	if err := c.Connect(ctx); err != nil {
		return managedClient{}, err
	}
	return managedClient{
		name:     name,
		closer:   c.Close,
		toolDefs: c.Tools,
		caller:   c,
	}, nil
}
