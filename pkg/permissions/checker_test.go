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

// --- new coverage tests ---

func TestChecker_AskFuncApproves(t *testing.T) {
	c := &Checker{
		Mode:           ModeDefault,
		NonInteractive: false,
		AskFunc:        func(_, _, _ string) bool { return true },
	}
	tool := &mockTool{name: "Bash"}
	d := c.Check(tool, json.RawMessage(`{"command":"echo hi"}`))
	if d.Behavior != BehaviorAllow {
		t.Errorf("AskFunc=true should allow, got %q", d.Behavior)
	}
}

func TestChecker_AskFuncDenies(t *testing.T) {
	c := &Checker{
		Mode:           ModeDefault,
		NonInteractive: false,
		AskFunc:        func(_, _, _ string) bool { return false },
	}
	tool := &mockTool{name: "Bash"}
	d := c.Check(tool, json.RawMessage(`{"command":"echo hi"}`))
	if d.Behavior != BehaviorDeny {
		t.Errorf("AskFunc=false should deny, got %q", d.Behavior)
	}
}

func TestChecker_InteractiveNoAskFunc(t *testing.T) {
	c := &Checker{Mode: ModeDefault, NonInteractive: false}
	tool := &mockTool{name: "Bash"}
	d := c.Check(tool, json.RawMessage(`{"command":"echo hi"}`))
	if d.Behavior != BehaviorAsk {
		t.Errorf("interactive with no AskFunc should ask, got %q", d.Behavior)
	}
}

func TestChecker_AskRule_NonInteractive(t *testing.T) {
	c := &Checker{
		Mode:           ModeDefault,
		NonInteractive: true,
		Rules:          config.PermissionRules{Ask: []string{"Bash(curl *)"}},
	}
	tool := &mockTool{name: "Bash"}
	d := c.Check(tool, json.RawMessage(`{"command":"curl http://example.com"}`))
	if d.Behavior != BehaviorDeny {
		t.Errorf("ask rule + non-interactive = deny, got %q", d.Behavior)
	}
}

func TestChecker_AskRule_WithAskFunc(t *testing.T) {
	c := &Checker{
		Mode:    ModeDefault,
		Rules:   config.PermissionRules{Ask: []string{"Bash(curl *)"}},
		AskFunc: func(_, _, _ string) bool { return true },
	}
	tool := &mockTool{name: "Bash"}
	d := c.Check(tool, json.RawMessage(`{"command":"curl http://example.com"}`))
	if d.Behavior != BehaviorAllow {
		t.Errorf("ask rule + AskFunc=true = allow, got %q", d.Behavior)
	}
}

func TestChecker_ReadOnlyInAdditionalDir(t *testing.T) {
	c := &Checker{
		Mode:       ModeDefault,
		CWD:        "/tmp/proj",
		AdditlDirs: []string{"/tmp/scratch"},
	}
	tool := &mockTool{name: "Read", readOnly: true}
	d := c.Check(tool, json.RawMessage(`{"file_path":"/tmp/scratch/notes.txt"}`))
	if d.Behavior != BehaviorAllow {
		t.Errorf("read in additionalDir should allow, got %q", d.Behavior)
	}
}

func TestChecker_ReadOnlyOutsideAllowed(t *testing.T) {
	c := &Checker{
		Mode: ModeDefault,
		CWD:  "/tmp/proj",
	}
	tool := &mockTool{name: "Read", readOnly: true}
	d := c.Check(tool, json.RawMessage(`{"file_path":"/etc/passwd"}`))
	// No allow rule → ask (interactive default)
	if d.Behavior == BehaviorAllow {
		t.Errorf("should not allow read outside CWD without rule, got allow")
	}
}

func TestChecker_AutoModeWriteInCWD(t *testing.T) {
	c := &Checker{
		Mode: ModeAuto,
		CWD:  "/tmp/proj",
	}
	tool := &mockTool{name: "Write", readOnly: false}
	d := c.Check(tool, json.RawMessage(`{"file_path":"/tmp/proj/output.go"}`))
	if d.Behavior != BehaviorAllow {
		t.Errorf("auto mode write in CWD should allow, got %q", d.Behavior)
	}
}

func TestChecker_DontAskModeWriteInCWD(t *testing.T) {
	c := &Checker{
		Mode: ModeDontAsk,
		CWD:  "/tmp/proj",
	}
	tool := &mockTool{name: "Edit", readOnly: false}
	d := c.Check(tool, json.RawMessage(`{"file_path":"/tmp/proj/main.go"}`))
	if d.Behavior != BehaviorAllow {
		t.Errorf("dontAsk mode write in CWD should allow, got %q", d.Behavior)
	}
}

func TestChecker_WriteOutsideCWD_AutoMode(t *testing.T) {
	c := &Checker{
		Mode:           ModeAuto,
		CWD:            "/tmp/proj",
		NonInteractive: true,
	}
	tool := &mockTool{name: "Write", readOnly: false}
	d := c.Check(tool, json.RawMessage(`{"file_path":"/tmp/other/file.go"}`))
	// Not in CWD, no allow rule → deny (non-interactive)
	if d.Behavior != BehaviorDeny {
		t.Errorf("auto mode write outside CWD + non-interactive should deny, got %q", d.Behavior)
	}
}

func TestChecker_DangerousFile(t *testing.T) {
	c := &Checker{
		Mode:           ModeDefault,
		NonInteractive: true,
		CWD:            "/home/user",
	}
	tool := &mockTool{name: "Write", readOnly: false}
	// .bashrc is in dangerousFiles list
	d := c.Check(tool, json.RawMessage(`{"file_path":"/home/user/.bashrc"}`))
	if d.Behavior != BehaviorDeny {
		t.Errorf("dangerous file .bashrc should deny, got %q", d.Behavior)
	}
}

func TestChecker_EmptyInput(t *testing.T) {
	c := &Checker{Mode: ModeBypassPermissions}
	tool := &mockTool{name: "Read", readOnly: true}
	d := c.Check(tool, nil)
	if d.Behavior != BehaviorAllow {
		t.Errorf("bypass with nil input should allow, got %q", d.Behavior)
	}
}

func TestExtractArg_BashCommand(t *testing.T) {
	arg := extractArg("Bash", json.RawMessage(`{"command":"ls -la"}`))
	if arg != "ls -la" {
		t.Errorf("got %q", arg)
	}
}

func TestExtractArg_FilePath(t *testing.T) {
	arg := extractArg("Read", json.RawMessage(`{"file_path":"/tmp/foo.go"}`))
	if arg != "/tmp/foo.go" {
		t.Errorf("got %q", arg)
	}
}

func TestExtractArg_Pattern(t *testing.T) {
	arg := extractArg("Grep", json.RawMessage(`{"pattern":"TODO"}`))
	if arg != "TODO" {
		t.Errorf("got %q", arg)
	}
}

func TestExtractArg_InvalidJSON(t *testing.T) {
	arg := extractArg("Bash", json.RawMessage(`not json`))
	if arg != "" {
		t.Errorf("expected empty for invalid JSON, got %q", arg)
	}
}

func TestExtractArg_EmptyInput(t *testing.T) {
	arg := extractArg("Bash", nil)
	if arg != "" {
		t.Errorf("expected empty for nil input, got %q", arg)
	}
}

func TestIsDangerousPath(t *testing.T) {
	cases := []struct {
		path      string
		dangerous bool
	}{
		{"/home/user/.git/config", true},
		{"/home/user/.bashrc", true},
		{"/home/user/.zshrc", true},
		{"/home/user/.gitconfig", true},
		{"/home/user/.mcp.json", true},
		{"/home/user/code/main.go", false},
		{"/tmp/output.txt", false},
		{"/home/user/.vscode/settings.json", true},
		{"/home/user/.idea/workspace.xml", true},
	}
	for _, tc := range cases {
		got := isDangerousPath(tc.path)
		if got != tc.dangerous {
			t.Errorf("isDangerousPath(%q) = %v, want %v", tc.path, got, tc.dangerous)
		}
	}
}

func TestIsAllowedPath_Empty(t *testing.T) {
	c := &Checker{CWD: "/tmp/proj"}
	if c.isAllowedPath("") {
		t.Error("empty path should not be allowed")
	}
}

func TestIsAllowedPath_ExactCWD(t *testing.T) {
	c := &Checker{CWD: "/tmp/proj"}
	if !c.isAllowedPath("/tmp/proj") {
		t.Error("exact CWD should be allowed")
	}
}
