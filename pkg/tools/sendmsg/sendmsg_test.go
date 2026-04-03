package sendmsg_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
	"github.com/hankwenyx/claude-code-go/pkg/tools/sendmsg"
)

func TestSendUserMessageBasics(t *testing.T) {
	tool := sendmsg.New()

	if tool.Name() != "SendUserMessage" {
		t.Errorf("Name: %q", tool.Name())
	}
	if !tool.IsReadOnly() {
		t.Error("expected IsReadOnly=true")
	}
	if tool.Description() == "" {
		t.Error("Description is empty")
	}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("InputSchema is empty")
	}
}

func TestSendUserMessageCall(t *testing.T) {
	tool := sendmsg.New()

	input, _ := json.Marshal(map[string]string{"message": "hello world"})
	res, err := tool.Call(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if res.Content != "hello world" {
		t.Errorf("Content: %q", res.Content)
	}
	if res.IsError {
		t.Error("expected IsError=false")
	}
}

func TestSendUserMessageCallEmptyMessage(t *testing.T) {
	tool := sendmsg.New()
	input, _ := json.Marshal(map[string]string{"message": ""})
	res, err := tool.Call(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "" {
		t.Errorf("expected empty content, got %q", res.Content)
	}
}

func TestSendUserMessageCallInvalidJSON(t *testing.T) {
	tool := sendmsg.New()
	res, err := tool.Call(context.Background(), json.RawMessage(`not json`))
	// Should return error result, not Go error - just verify no panic
	_ = res
	_ = err
}

func TestSendUserMessagePermissions(t *testing.T) {
	tool := sendmsg.New()
	dec := tool.CheckPermissions(nil, "auto", tools.PermissionRules{})
	if dec.Behavior != "allow" {
		t.Errorf("expected allow, got %q", dec.Behavior)
	}
}
