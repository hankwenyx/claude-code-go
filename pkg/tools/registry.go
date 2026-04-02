package tools

import (
	"github.com/hankwenyx/claude-code-go/pkg/api"
)

// Registry holds all registered tools
type Registry struct {
	tools []Tool
}

// NewRegistry creates a new tool registry
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a tool to the registry
func (r *Registry) Register(t Tool) {
	r.tools = append(r.tools, t)
}

// All returns all registered tools
func (r *Registry) All() []Tool {
	return r.tools
}

// Get returns a tool by name
func (r *Registry) Get(name string) Tool {
	for _, t := range r.tools {
		if t.Name() == name {
			return t
		}
	}
	return nil
}

// ToAPIDefs converts all tools to API ToolDef format
func (r *Registry) ToAPIDefs() []api.ToolDef {
	defs := make([]api.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, api.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return defs
}
