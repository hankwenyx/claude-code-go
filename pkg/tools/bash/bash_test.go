package bash

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

func TestBash_Echo(t *testing.T) {
	tool := New()
	input := json.RawMessage(`{"command":"echo hello"}`)
	result, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Errorf("expected 'hello' in output, got: %q", result.Content)
	}
}

func TestBash_ExitCode(t *testing.T) {
	tool := New()
	input := json.RawMessage(`{"command":"exit 1"}`)
	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected IsError=true for exit 1")
	}
}

func TestBash_TruncatesLargeOutput(t *testing.T) {
	tool := New()
	// Generate output > 30000 chars
	input := json.RawMessage(`{"command":"python3 -c \"print('x'*40000)\""}`)
	result, _ := tool.Call(context.Background(), input)
	if len(result.Content) > maxOutputChars+100 {
		t.Errorf("output not truncated: len=%d", len(result.Content))
	}
}

func TestBash_Permissions_AllowRule(t *testing.T) {
	tool := New()
	input := json.RawMessage(`{"command":"git status"}`)
	d := tool.CheckPermissions(input, "default", tools.PermissionRules{
		Allow: []string{"Bash(git *)"},
	})
	if d.Behavior != "allow" {
		t.Errorf("expected allow, got %q", d.Behavior)
	}
}

func TestBash_Permissions_DenyRule(t *testing.T) {
	tool := New()
	input := json.RawMessage(`{"command":"rm -rf /"}`)
	d := tool.CheckPermissions(input, "default", tools.PermissionRules{
		Deny: []string{"Bash(rm*)"},
	})
	if d.Behavior != "deny" {
		t.Errorf("expected deny, got %q", d.Behavior)
	}
}
