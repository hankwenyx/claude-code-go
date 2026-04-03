package todo_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hankwenyx/claude-code-go/pkg/tools/todo"
)

// --- Store ---

func TestStore_WriteAndRead(t *testing.T) {
	s := todo.NewStore()

	items := []todo.Item{
		{Subject: "Task A"},
		{Subject: "Task B", Status: "in_progress"},
	}
	result := s.Write(items)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	// IDs should be assigned
	if result[0].ID == "" || result[1].ID == "" {
		t.Error("IDs not assigned")
	}
	// Default status
	if result[0].Status != "pending" {
		t.Errorf("expected pending, got %q", result[0].Status)
	}
	if result[1].Status != "in_progress" {
		t.Errorf("expected in_progress, got %q", result[1].Status)
	}

	got := s.Read()
	if len(got) != 2 {
		t.Fatalf("Read: expected 2, got %d", len(got))
	}
}

func TestStore_WriteReplaces(t *testing.T) {
	s := todo.NewStore()
	s.Write([]todo.Item{{Subject: "old"}})
	s.Write([]todo.Item{{Subject: "new1"}, {Subject: "new2"}})
	items := s.Read()
	if len(items) != 2 {
		t.Fatalf("expected 2 after replace, got %d", len(items))
	}
}

func TestStore_WriteEmpty(t *testing.T) {
	s := todo.NewStore()
	s.Write([]todo.Item{{Subject: "x"}})
	s.Write([]todo.Item{})
	if len(s.Read()) != 0 {
		t.Error("expected empty list after writing empty")
	}
}

func TestStore_PreserveExistingID(t *testing.T) {
	s := todo.NewStore()
	result := s.Write([]todo.Item{{Subject: "t"}})
	existingID := result[0].ID
	// Write again preserving the ID
	s.Write([]todo.Item{{ID: existingID, Subject: "updated"}})
	items := s.Read()
	if items[0].ID != existingID {
		t.Errorf("ID changed: want %q, got %q", existingID, items[0].ID)
	}
}

// --- TodoWrite tool ---

func TestTodoWriteTool(t *testing.T) {
	store := todo.NewStore()
	wt := todo.NewWriteTool(store)

	if wt.Name() != "TodoWrite" {
		t.Errorf("Name: %q", wt.Name())
	}
	if wt.IsReadOnly() {
		t.Error("expected IsReadOnly=false")
	}
	if wt.Description() == "" {
		t.Error("Description empty")
	}
	if len(wt.InputSchema()) == 0 {
		t.Error("InputSchema empty")
	}

	input, _ := json.Marshal(map[string]interface{}{
		"todos": []map[string]interface{}{
			{"subject": "Fix bug"},
			{"subject": "Write tests", "status": "in_progress"},
		},
	})
	res, err := wt.Call(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Errorf("unexpected error: %s", res.Content)
	}

	// Content should be JSON array
	var items []todo.Item
	if err := json.Unmarshal([]byte(res.Content), &items); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2, got %d", len(items))
	}
}

func TestTodoWriteTool_InvalidJSON(t *testing.T) {
	store := todo.NewStore()
	wt := todo.NewWriteTool(store)
	_, err := wt.Call(context.Background(), []byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTodoWriteTool_Permissions(t *testing.T) {
	store := todo.NewStore()
	wt := todo.NewWriteTool(store)
	dec := wt.CheckPermissions(nil, "default", todo.PermissionRulesStub())
	if dec.Behavior != "allow" {
		t.Errorf("expected allow, got %q", dec.Behavior)
	}
}

// --- TodoRead tool ---

func TestTodoReadTool(t *testing.T) {
	store := todo.NewStore()
	store.Write([]todo.Item{{Subject: "Buy milk"}, {Subject: "Walk dog"}})

	rt := todo.NewReadTool(store)
	if rt.Name() != "TodoRead" {
		t.Errorf("Name: %q", rt.Name())
	}
	if !rt.IsReadOnly() {
		t.Error("expected IsReadOnly=true")
	}

	res, err := rt.Call(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Errorf("unexpected error: %s", res.Content)
	}

	var items []todo.Item
	if err := json.Unmarshal([]byte(res.Content), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2, got %d", len(items))
	}
	if items[0].Subject != "Buy milk" {
		t.Errorf("subject: %q", items[0].Subject)
	}
}

func TestTodoReadTool_EmptyStore(t *testing.T) {
	store := todo.NewStore()
	rt := todo.NewReadTool(store)
	res, err := rt.Call(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Should return "[]" or "null"
	if res.Content != "[]" && res.Content != "null" {
		t.Errorf("unexpected content for empty store: %q", res.Content)
	}
}

func TestTodoReadTool_Permissions(t *testing.T) {
	rt := todo.NewReadTool(todo.NewStore())
	dec := rt.CheckPermissions(nil, "auto", todo.PermissionRulesStub())
	if dec.Behavior != "allow" {
		t.Errorf("expected allow, got %q", dec.Behavior)
	}
}

// --- shared store between write and read ---

func TestWriteRead_SharedStore(t *testing.T) {
	store := todo.NewStore()
	wt := todo.NewWriteTool(store)
	rt := todo.NewReadTool(store)

	input, _ := json.Marshal(map[string]interface{}{
		"todos": []map[string]string{{"subject": "Shared item"}},
	})
	if _, err := wt.Call(context.Background(), input); err != nil {
		t.Fatal(err)
	}

	res, err := rt.Call(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var items []todo.Item
	json.Unmarshal([]byte(res.Content), &items)
	if len(items) != 1 || items[0].Subject != "Shared item" {
		t.Errorf("unexpected items: %+v", items)
	}
}
