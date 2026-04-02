package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// mockTool implements Tool for testing
type mockTool struct {
	name        string
	description string
	schema      json.RawMessage
	readOnly    bool
}

func (m *mockTool) Name() string                     { return m.name }
func (m *mockTool) Description() string              { return m.description }
func (m *mockTool) InputSchema() json.RawMessage     { return m.schema }
func (m *mockTool) IsReadOnly() bool                 { return m.readOnly }
func (m *mockTool) Call(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	return ToolResult{Content: "mock result"}, nil
}
func (m *mockTool) CheckPermissions(input json.RawMessage, mode string, rules PermissionRules) PermissionDecision {
	return PermissionDecision{Behavior: "allow", Reason: "mock"}
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.tools) != 0 {
		t.Errorf("expected empty registry, got %d tools", len(r.tools))
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()
	tool := &mockTool{name: "TestTool"}

	r.Register(tool)

	if len(r.tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(r.tools))
	}
}

func TestRegistry_Register_Multiple(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "Tool1"})
	r.Register(&mockTool{name: "Tool2"})
	r.Register(&mockTool{name: "Tool3"})

	if len(r.tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(r.tools))
	}
}

func TestRegistry_All(t *testing.T) {
	r := NewRegistry()
	tool1 := &mockTool{name: "Tool1"}
	tool2 := &mockTool{name: "Tool2"}
	r.Register(tool1)
	r.Register(tool2)

	all := r.All()
	if len(all) != 2 {
		t.Errorf("expected 2 tools, got %d", len(all))
	}

	// Verify order is preserved
	if all[0].Name() != "Tool1" || all[1].Name() != "Tool2" {
		t.Errorf("tool order not preserved")
	}
}

func TestRegistry_Get_Found(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "Bash"})
	r.Register(&mockTool{name: "Read"})

	tool := r.Get("Bash")
	if tool == nil {
		t.Fatal("expected to find Bash tool")
	}
	if tool.Name() != "Bash" {
		t.Errorf("got tool name %q", tool.Name())
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "Bash"})

	tool := r.Get("NonExistent")
	if tool != nil {
		t.Errorf("expected nil for non-existent tool, got %v", tool)
	}
}

func TestRegistry_Get_CaseSensitive(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "Bash"})

	// Should be case-sensitive
	tool := r.Get("bash")
	if tool != nil {
		t.Error("expected nil for case-mismatched name")
	}
}

func TestRegistry_ToAPIDefs(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{
		name:        "TestTool",
		description: "A test tool",
		schema:      json.RawMessage(`{"type":"object"}`),
	})
	r.Register(&mockTool{
		name:        "AnotherTool",
		description: "Another test tool",
		schema:      json.RawMessage(`{"type":"string"}`),
	})

	defs := r.ToAPIDefs()
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}

	if defs[0].Name != "TestTool" {
		t.Errorf("def[0].Name: got %q", defs[0].Name)
	}
	if defs[0].Description != "A test tool" {
		t.Errorf("def[0].Description: got %q", defs[0].Description)
	}
	if string(defs[0].InputSchema) != `{"type":"object"}` {
		t.Errorf("def[0].InputSchema: got %s", defs[0].InputSchema)
	}
}

func TestRegistry_ToAPIDefs_Empty(t *testing.T) {
	r := NewRegistry()
	defs := r.ToAPIDefs()
	if defs == nil {
		t.Error("expected non-nil slice")
	}
	if len(defs) != 0 {
		t.Errorf("expected empty slice, got %d", len(defs))
	}
}
