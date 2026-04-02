package permissions

import (
	"encoding/json"
	"testing"

	"github.com/hankwenyx/claude-code-go/pkg/config"
)

type mockTool struct {
	name     string
	readOnly bool
}

func (m *mockTool) Name() string     { return m.name }
func (m *mockTool) IsReadOnly() bool { return m.readOnly }

func TestChecker_BypassMode(t *testing.T) {
	c := &Checker{Mode: ModeBypassPermissions}
	tool := &mockTool{name: "Bash"}
	d := c.Check(tool, json.RawMessage(`{"command":"rm -rf /"}`))
	if d.Behavior != BehaviorAllow {
		t.Errorf("bypass mode should allow everything, got %q", d.Behavior)
	}
}

func TestChecker_DenyRule(t *testing.T) {
	c := &Checker{
		Mode:           ModeDefault,
		NonInteractive: true,
		Rules: config.PermissionRules{
			Deny: []string{"Bash(rm *)"},
		},
	}
	tool := &mockTool{name: "Bash"}
	d := c.Check(tool, json.RawMessage(`{"command":"rm -rf /"}`))
	if d.Behavior != BehaviorDeny {
		t.Errorf("should deny rm, got %q: %s", d.Behavior, d.Reason)
	}
}

func TestChecker_AllowRule(t *testing.T) {
	c := &Checker{
		Mode: ModeDefault,
		Rules: config.PermissionRules{
			Allow: []string{"Bash(git *)"},
		},
	}
	tool := &mockTool{name: "Bash"}
	d := c.Check(tool, json.RawMessage(`{"command":"git status"}`))
	if d.Behavior != BehaviorAllow {
		t.Errorf("should allow git, got %q: %s", d.Behavior, d.Reason)
	}
}

func TestChecker_NonInteractiveAskToDeny(t *testing.T) {
	c := &Checker{
		Mode:           ModeDefault,
		NonInteractive: true,
	}
	tool := &mockTool{name: "Bash"}
	d := c.Check(tool, json.RawMessage(`{"command":"echo hello"}`))
	if d.Behavior != BehaviorDeny {
		t.Errorf("non-interactive ask should become deny, got %q", d.Behavior)
	}
}

func TestChecker_ReadOnlyInCWD(t *testing.T) {
	c := &Checker{
		Mode: ModeDefault,
		CWD:  "/tmp/myproject",
	}
	tool := &mockTool{name: "Read", readOnly: true}
	d := c.Check(tool, json.RawMessage(`{"file_path":"/tmp/myproject/main.go"}`))
	if d.Behavior != BehaviorAllow {
		t.Errorf("read in cwd should allow, got %q: %s", d.Behavior, d.Reason)
	}
}

func TestChecker_DangerousPath(t *testing.T) {
	c := &Checker{
		Mode:           ModeDefault,
		NonInteractive: true,
		CWD:            "/tmp/myproject",
	}
	tool := &mockTool{name: "Write", readOnly: false}
	d := c.Check(tool, json.RawMessage(`{"file_path":"/tmp/myproject/.git/config"}`))
	if d.Behavior != BehaviorDeny {
		t.Errorf("dangerous path should deny, got %q: %s", d.Behavior, d.Reason)
	}
}
