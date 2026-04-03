package task_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"encoding/json"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
	agentTask "github.com/hankwenyx/claude-code-go/pkg/tools/task"
)

// --- sync Task tool ---

func TestTaskTool_SyncSuccess(t *testing.T) {
	runner := func(_ context.Context, prompt string) (string, error) {
		return "done: " + prompt, nil
	}
	tool := agentTask.New(runner)

	input, _ := json.Marshal(map[string]string{
		"description": "say hi",
		"prompt":      "hello",
	})
	res, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Errorf("unexpected IsError: %s", res.Content)
	}
	if res.Content != "done: hello" {
		t.Errorf("content: %q", res.Content)
	}
}

func TestTaskTool_SyncRunnerError(t *testing.T) {
	runner := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("sub-agent failed")
	}
	tool := agentTask.New(runner)

	input, _ := json.Marshal(map[string]string{"description": "d", "prompt": "p"})
	res, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatal(err) // Go error should be nil; failure surfaced via IsError
	}
	if !res.IsError {
		t.Error("expected IsError=true on runner failure")
	}
}

func TestTaskTool_MissingPrompt(t *testing.T) {
	tool := agentTask.New(func(_ context.Context, _ string) (string, error) { return "", nil })
	input, _ := json.Marshal(map[string]string{"description": "d"})
	_, err := tool.Call(context.Background(), input)
	if err == nil {
		t.Error("expected error for empty prompt")
	}
}

func TestTaskTool_InvalidJSON(t *testing.T) {
	tool := agentTask.New(func(_ context.Context, _ string) (string, error) { return "", nil })
	_, err := tool.Call(context.Background(), json.RawMessage(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTaskTool_Basics(t *testing.T) {
	tool := agentTask.New(func(_ context.Context, _ string) (string, error) { return "", nil })
	if tool.Name() != "Task" {
		t.Errorf("Name: %q", tool.Name())
	}
	if tool.IsReadOnly() {
		t.Error("expected IsReadOnly=false")
	}
	if tool.Description() == "" {
		t.Error("Description empty")
	}
	if len(tool.InputSchema()) == 0 {
		t.Error("InputSchema empty")
	}
}

func TestTaskTool_Permissions(t *testing.T) {
	tool := agentTask.New(func(_ context.Context, _ string) (string, error) { return "", nil })

	cases := []struct {
		rules    tools.PermissionRules
		mode     string
		behavior string
	}{
		{tools.PermissionRules{Deny: []string{"Task"}}, "auto", "deny"},
		{tools.PermissionRules{Allow: []string{"Task"}}, "auto", "allow"},
		{tools.PermissionRules{}, "bypassPermissions", "allow"},
		{tools.PermissionRules{}, "auto", "ask"},
	}
	for _, tc := range cases {
		dec := tool.CheckPermissions(nil, tc.mode, tc.rules)
		if dec.Behavior != tc.behavior {
			t.Errorf("mode=%s rules=%+v: want %q got %q", tc.mode, tc.rules, tc.behavior, dec.Behavior)
		}
	}
}

// --- async: Manager ---

func TestManager_DispatchAndGet(t *testing.T) {
	m := agentTask.NewManager(4)
	done := make(chan struct{})
	runner := func(_ context.Context, _ string) (string, error) {
		<-done
		return "async result", nil
	}
	id := m.Dispatch(context.Background(), "bg task", "do something", runner)
	if id == "" {
		t.Fatal("empty task ID")
	}
	task := m.Get(id)
	if task == nil {
		t.Fatal("task not found")
	}
	if task.Status != agentTask.StatusRunning {
		t.Errorf("expected running, got %q", task.Status)
	}

	close(done)
	// wait for goroutine to finish
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if t2 := m.Get(id); t2 != nil && t2.Status == agentTask.StatusCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m.Get(id).Status != agentTask.StatusCompleted {
		t.Error("expected completed")
	}
}

func TestManager_Notification(t *testing.T) {
	m := agentTask.NewManager(4)
	runner := func(_ context.Context, _ string) (string, error) {
		return "done", nil
	}
	m.Dispatch(context.Background(), "notif task", "p", runner)

	var notif *agentTask.Notification
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ns := m.DrainNotifications()
		if len(ns) > 0 {
			n := ns[0]
			notif = &n
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if notif == nil {
		t.Fatal("no notification received")
	}
	if notif.Task.Result != "done" {
		t.Errorf("result: %q", notif.Task.Result)
	}
}

func TestManager_FailedTask(t *testing.T) {
	m := agentTask.NewManager(4)
	runner := func(_ context.Context, _ string) (string, error) {
		return "", fmt.Errorf("exploded")
	}
	id := m.Dispatch(context.Background(), "fail", "p", runner)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if t2 := m.Get(id); t2 != nil && t2.Status == agentTask.StatusFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	task := m.Get(id)
	if task.Status != agentTask.StatusFailed {
		t.Errorf("expected failed, got %q", task.Status)
	}
	if task.Err == nil || task.Err.Error() != "exploded" {
		t.Errorf("err: %v", task.Err)
	}
}

func TestManager_GetUnknown(t *testing.T) {
	m := agentTask.NewManager(4)
	if m.Get("nonexistent") != nil {
		t.Error("expected nil for unknown id")
	}
}

// --- async via Task tool ---

func TestTaskTool_AsyncDispatch(t *testing.T) {
	m := agentTask.NewManager(4)
	block := make(chan struct{})
	runner := func(_ context.Context, _ string) (string, error) {
		<-block
		return "async done", nil
	}
	tool := agentTask.NewWithManager(runner, m)

	input, _ := json.Marshal(map[string]interface{}{
		"description": "bg",
		"prompt":      "do work",
		"async":       true,
	})
	res, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Errorf("unexpected error: %s", res.Content)
	}
	// Result should contain task_id and status=running
	if !contains(res.Content, "running") {
		t.Errorf("expected 'running' in content: %q", res.Content)
	}
	close(block)
}

func TestTaskTool_AsyncFallsBackToSync_NoManager(t *testing.T) {
	runner := func(_ context.Context, _ string) (string, error) { return "sync result", nil }
	tool := agentTask.New(runner) // no manager
	input, _ := json.Marshal(map[string]interface{}{
		"description": "d",
		"prompt":      "p",
		"async":       true,
	})
	res, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	// Falls back to sync since no manager
	if res.Content != "sync result" {
		t.Errorf("expected sync result, got %q", res.Content)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
