// Package todo provides TodoWrite and TodoRead tools for in-session task tracking.
// A single Store is shared between the two tools so they see the same list.
package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

// Status values for a todo item.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
)

// Item is a single todo entry.
type Item struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"` // pending | in_progress | completed
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store is a thread-safe in-memory todo list, shared by TodoWrite and TodoRead.
type Store struct {
	mu    sync.RWMutex
	items []Item
	seq   int
}

// NewStore creates an empty Store.
func NewStore() *Store { return &Store{} }

// Write atomically replaces the list with the provided items, assigning IDs to
// any entry whose ID is empty. Returns the final list.
func (s *Store) Write(items []Item) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i := range items {
		if items[i].ID == "" {
			s.seq++
			items[i].ID = fmt.Sprintf("%d", s.seq)
			items[i].CreatedAt = now
		}
		if items[i].Status == "" {
			items[i].Status = StatusPending
		}
		items[i].UpdatedAt = now
	}
	s.items = make([]Item, len(items))
	copy(s.items, items)
	return s.items
}

// Read returns a snapshot of the current list.
func (s *Store) Read() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Item, len(s.items))
	copy(out, s.items)
	return out
}

// --- TodoWrite tool ---

// WriteInput is the JSON schema for TodoWrite.
type WriteInput struct {
	Todos []Item `json:"todos"`
}

// WriteTool replaces the session todo list.
type WriteTool struct{ store *Store }

func NewWriteTool(store *Store) *WriteTool { return &WriteTool{store: store} }

func (t *WriteTool) Name() string     { return "TodoWrite" }
func (t *WriteTool) IsReadOnly() bool { return false }

func (t *WriteTool) Description() string {
	return "Write (replace) the session todo list. " +
		"Pass the full updated list; items without an 'id' get one assigned automatically. " +
		"Use this to create, update, or delete tasks."
}

func (t *WriteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"todos": {
				"type": "array",
				"description": "Complete replacement list of todo items",
				"items": {
					"type": "object",
					"properties": {
						"id":          {"type": "string"},
						"subject":     {"type": "string"},
						"description":{"type": "string"},
						"status":      {"type": "string", "enum": ["pending","in_progress","completed"]}
					},
					"required": ["subject"]
				}
			}
		},
		"required": ["todos"]
	}`)
}

func (t *WriteTool) Call(_ context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	var in WriteInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return tools.ToolResult{IsError: true}, fmt.Errorf("TodoWrite: %w", err)
	}
	result := t.store.Write(in.Todos)
	out, _ := json.MarshalIndent(result, "", "  ")
	return tools.ToolResult{Content: string(out)}, nil
}

func (t *WriteTool) CheckPermissions(_ json.RawMessage, _ string, _ tools.PermissionRules) tools.PermissionDecision {
	return tools.PermissionDecision{Behavior: "allow", Reason: "in-session state only"}
}

// --- TodoRead tool ---

// ReadTool returns the current session todo list.
type ReadTool struct{ store *Store }

func NewReadTool(store *Store) *ReadTool { return &ReadTool{store: store} }

func (t *ReadTool) Name() string     { return "TodoRead" }
func (t *ReadTool) IsReadOnly() bool { return true }

func (t *ReadTool) Description() string {
	return "Read the current session todo list. Returns all tasks with their IDs, subjects, and status."
}

func (t *ReadTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *ReadTool) Call(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	items := t.store.Read()
	out, _ := json.MarshalIndent(items, "", "  ")
	return tools.ToolResult{Content: string(out)}, nil
}

func (t *ReadTool) CheckPermissions(_ json.RawMessage, _ string, _ tools.PermissionRules) tools.PermissionDecision {
	return tools.PermissionDecision{Behavior: "allow", Reason: "read-only in-session state"}
}

// PermissionRulesStub returns an empty PermissionRules for use in tests.
func PermissionRulesStub() tools.PermissionRules { return tools.PermissionRules{} }
