package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

// Caller is the interface implemented by both StdioClient and SSEClient.
// It is exported so external test packages can provide a fake implementation.
type Caller interface {
	CallTool(ctx context.Context, mcpName string, args json.RawMessage) (string, bool, error)
}

// caller is kept as an alias for internal use.
type caller = Caller

// mcpTool adapts a single MCP tool definition to the tools.Tool interface.
// The registered name is "mcp__serverName__originalName" so the model uses the prefixed form.
type mcpTool struct {
	serverName     string
	def            ToolDef
	client         caller
	registeredName string // mcp__serverName__originalName
}

func newMCPTool(serverName string, def ToolDef, client caller) *mcpTool {
	return &mcpTool{
		serverName:     serverName,
		def:            def,
		client:         client,
		registeredName: "mcp__" + serverName + "__" + def.Name,
	}
}

// NewToolForTest is exported for use in tests outside this package.
func NewToolForTest(serverName string, def ToolDef, client caller) tools.Tool {
	return newMCPTool(serverName, def, client)
}

func (t *mcpTool) Name() string        { return t.registeredName }
func (t *mcpTool) Description() string { return t.def.Description }
func (t *mcpTool) IsReadOnly() bool    { return false } // conservative default

func (t *mcpTool) InputSchema() json.RawMessage {
	if t.def.InputSchema != nil {
		return t.def.InputSchema
	}
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

// Call delegates to the MCP server. The input JSON is passed through as-is.
func (t *mcpTool) Call(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	content, isError, err := t.client.CallTool(ctx, t.def.Name, input)
	if err != nil {
		return tools.ToolResult{IsError: true}, err
	}
	return tools.ToolResult{Content: content, IsError: isError}, nil
}

// CheckPermissions applies the session permission rules to this MCP tool.
// Rule format: "mcp__serverName__toolName" or just "mcp__serverName" to match all tools on a server.
func (t *mcpTool) CheckPermissions(input json.RawMessage, mode string, rules tools.PermissionRules) tools.PermissionDecision {
	name := t.registeredName
	serverPrefix := "mcp__" + t.serverName

	// Check deny rules first.
	for _, rule := range rules.Deny {
		if matchMCPRule(rule, name, serverPrefix) {
			return tools.PermissionDecision{Behavior: "deny", Reason: "matched deny rule: " + rule}
		}
	}
	// Check ask rules.
	for _, rule := range rules.Ask {
		if matchMCPRule(rule, name, serverPrefix) {
			return tools.PermissionDecision{Behavior: "ask", Reason: "matched ask rule: " + rule}
		}
	}
	// Check allow rules.
	for _, rule := range rules.Allow {
		if matchMCPRule(rule, name, serverPrefix) {
			return tools.PermissionDecision{Behavior: "allow", Reason: "matched allow rule: " + rule}
		}
	}
	// Default: ask (or deny in non-interactive mode — the caller handles that).
	return tools.PermissionDecision{Behavior: "ask", Reason: "mcp tool requires permission"}
}

// matchMCPRule checks whether a permission rule string matches an MCP tool.
// Supports exact match, server wildcard, and trailing "*" glob.
func matchMCPRule(rule, fullName, serverPrefix string) bool {
	// Exact: "mcp__fs__read_file"
	if rule == fullName {
		return true
	}
	// Server wildcard: "mcp__fs" or "mcp__fs__*"
	if rule == serverPrefix || rule == serverPrefix+"__*" {
		return true
	}
	// Trailing glob on full name: "mcp__fs__read*"
	if strings.HasSuffix(rule, "*") {
		prefix := strings.TrimSuffix(rule, "*")
		return strings.HasPrefix(fullName, prefix)
	}
	return false
}
