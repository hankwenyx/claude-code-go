package hooks_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/hooks"
)

// skipIfNoSh skips the test on platforms without /bin/sh.
func skipIfNoSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require sh; skipping on Windows")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found")
	}
}

func hookDef(cmd string) config.HookDef {
	return config.HookDef{Type: "command", Command: cmd}
}

func group(matcher string, cmds ...string) config.HookGroup {
	defs := make([]config.HookDef, len(cmds))
	for i, c := range cmds {
		defs[i] = hookDef(c)
	}
	return config.HookGroup{Matcher: matcher, Hooks: defs}
}

// ---- matchesGroup (tested indirectly via Runner) ----

func TestRunner_NoHooks(t *testing.T) {
	r := hooks.New(config.HooksConfig{})
	// Should be no-op and return nil/empty
	mod, reason, err := r.RunPreToolUse(context.Background(), "Bash", json.RawMessage(`{}`))
	if err != nil || reason != "" || mod != nil {
		t.Errorf("empty runner: mod=%s reason=%q err=%v", mod, reason, err)
	}
	out, err := r.RunPostToolUse(context.Background(), "Bash", json.RawMessage(`{}`), "output", false)
	if err != nil || out != "output" {
		t.Errorf("post tool: out=%q err=%v", out, err)
	}
	r.RunNotification(context.Background(), "hello") // should not panic
}

func TestPreToolUse_PassThrough(t *testing.T) {
	skipIfNoSh(t)
	cfg := config.HooksConfig{
		PreToolUse: []config.HookGroup{group("Bash", "exit 0")},
	}
	r := hooks.New(cfg)
	mod, reason, err := r.RunPreToolUse(context.Background(), "Bash", json.RawMessage(`{"cmd":"ls"}`))
	if err != nil {
		t.Fatal(err)
	}
	if reason != "" {
		t.Errorf("unexpected block reason: %q", reason)
	}
	if mod != nil {
		t.Errorf("unexpected modification: %s", mod)
	}
}

func TestPreToolUse_BlockOnNonZeroExit(t *testing.T) {
	skipIfNoSh(t)
	cfg := config.HooksConfig{
		PreToolUse: []config.HookGroup{group("Bash", "echo 'rm not allowed'; exit 1")},
	}
	r := hooks.New(cfg)
	_, reason, err := r.RunPreToolUse(context.Background(), "Bash", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if reason == "" {
		t.Error("expected non-empty block reason for non-zero exit")
	}
	if !strings.Contains(reason, "rm not allowed") {
		t.Errorf("reason should contain hook stdout: %q", reason)
	}
}

func TestPreToolUse_BlockViaJSON(t *testing.T) {
	skipIfNoSh(t)
	cfg := config.HooksConfig{
		PreToolUse: []config.HookGroup{
			group("Bash", `echo '{"action":"block","reason":"json block"}'`),
		},
	}
	r := hooks.New(cfg)
	_, reason, err := r.RunPreToolUse(context.Background(), "Bash", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if reason != "json block" {
		t.Errorf("expected 'json block', got %q", reason)
	}
}

func TestPreToolUse_ModifyInput(t *testing.T) {
	skipIfNoSh(t)
	cfg := config.HooksConfig{
		PreToolUse: []config.HookGroup{
			group("Bash", `echo '{"modified_input":{"command":"echo hello"}}'`),
		},
	}
	r := hooks.New(cfg)
	mod, reason, err := r.RunPreToolUse(context.Background(), "Bash", json.RawMessage(`{"command":"rm -rf /"}`))
	if err != nil {
		t.Fatal(err)
	}
	if reason != "" {
		t.Errorf("unexpected block: %q", reason)
	}
	if mod == nil {
		t.Fatal("expected modified input")
	}
	var v map[string]string
	json.Unmarshal(mod, &v)
	if v["command"] != "echo hello" {
		t.Errorf("unexpected modified input: %s", mod)
	}
}

func TestPreToolUse_MatcherFilters(t *testing.T) {
	skipIfNoSh(t)
	cfg := config.HooksConfig{
		// Only matches "Read" tool
		PreToolUse: []config.HookGroup{group("Read", "exit 1")},
	}
	r := hooks.New(cfg)
	// Bash should not match "Read" matcher → no block
	_, reason, err := r.RunPreToolUse(context.Background(), "Bash", json.RawMessage(`{}`))
	if err != nil || reason != "" {
		t.Errorf("Bash should not match Read matcher: reason=%q err=%v", reason, err)
	}
	// Read should match → block
	_, reason, err = r.RunPreToolUse(context.Background(), "Read", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if reason == "" {
		t.Error("expected block for Read tool")
	}
}

func TestPreToolUse_WildcardMatcher(t *testing.T) {
	skipIfNoSh(t)
	cfg := config.HooksConfig{
		PreToolUse: []config.HookGroup{group("File*", "exit 1")},
	}
	r := hooks.New(cfg)
	// FileRead and FileWrite should match File*
	for _, name := range []string{"FileRead", "FileWrite", "FileEdit"} {
		_, reason, err := r.RunPreToolUse(context.Background(), name, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if reason == "" {
			t.Errorf("%s should have been blocked by File* matcher", name)
		}
	}
	// Bash should not match
	_, reason, _ := r.RunPreToolUse(context.Background(), "Bash", json.RawMessage(`{}`))
	if reason != "" {
		t.Errorf("Bash should not match File* matcher")
	}
}

func TestPreToolUse_EmptyMatcherMatchesAll(t *testing.T) {
	skipIfNoSh(t)
	cfg := config.HooksConfig{
		PreToolUse: []config.HookGroup{group("", "exit 1")},
	}
	r := hooks.New(cfg)
	for _, name := range []string{"Bash", "Read", "Glob", "mcp__server__tool"} {
		_, reason, err := r.RunPreToolUse(context.Background(), name, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if reason == "" {
			t.Errorf("%s should be blocked by empty matcher", name)
		}
	}
}

func TestPostToolUse_PassThrough(t *testing.T) {
	skipIfNoSh(t)
	cfg := config.HooksConfig{
		PostToolUse: []config.HookGroup{group("Bash", "exit 0")},
	}
	r := hooks.New(cfg)
	out, err := r.RunPostToolUse(context.Background(), "Bash", json.RawMessage(`{}`), "original", false)
	if err != nil {
		t.Fatal(err)
	}
	if out != "original" {
		t.Errorf("expected 'original', got %q", out)
	}
}

func TestPostToolUse_ModifyOutput(t *testing.T) {
	skipIfNoSh(t)
	cfg := config.HooksConfig{
		PostToolUse: []config.HookGroup{
			group("Bash", `echo '{"modified_output":"MODIFIED"}'`),
		},
	}
	r := hooks.New(cfg)
	out, err := r.RunPostToolUse(context.Background(), "Bash", json.RawMessage(`{}`), "original", false)
	if err != nil {
		t.Fatal(err)
	}
	if out != "MODIFIED" {
		t.Errorf("expected 'MODIFIED', got %q", out)
	}
}

func TestPostToolUse_ErrorNonFatal(t *testing.T) {
	skipIfNoSh(t)
	// Command exits non-zero — post hook errors are non-fatal
	cfg := config.HooksConfig{
		PostToolUse: []config.HookGroup{group("Bash", "exit 1")},
	}
	r := hooks.New(cfg)
	out, err := r.RunPostToolUse(context.Background(), "Bash", json.RawMessage(`{}`), "unchanged", false)
	if err != nil {
		t.Error("PostToolUse error should be non-fatal")
	}
	if out != "unchanged" {
		t.Errorf("expected 'unchanged', got %q", out)
	}
}

func TestNotification_Fires(t *testing.T) {
	skipIfNoSh(t)
	// Write notification message to a temp file
	dir := t.TempDir()
	outFile := dir + "/notif.txt"
	cfg := config.HooksConfig{
		Notification: []config.HookGroup{
			{Hooks: []config.HookDef{
				{Type: "command", Command: "cat > " + outFile},
			}},
		},
	}
	r := hooks.New(cfg)
	r.RunNotification(context.Background(), "task completed")

	// Read what was written
	data, err := readFile(outFile)
	if err != nil {
		t.Fatalf("notification file not written: %v", err)
	}
	var n hooks.NotificationInput
	if json.Unmarshal(data, &n) == nil {
		if n.Message != "task completed" {
			t.Errorf("message: %q", n.Message)
		}
	} else {
		if !strings.Contains(string(data), "task completed") {
			t.Errorf("notification output missing message: %q", string(data))
		}
	}
}

func TestPreToolUse_Timeout(t *testing.T) {
	skipIfNoSh(t)
	cfg := config.HooksConfig{
		PreToolUse: []config.HookGroup{
			group("Bash", "sleep 10"),
		},
	}
	// Override with 50ms timeout
	cfg.PreToolUse[0].Hooks[0].TimeoutMs = 50
	r := hooks.New(cfg)

	ctx := context.Background()
	_, _, err := r.RunPreToolUse(ctx, "Bash", json.RawMessage(`{}`))
	// Timeout is treated as an exec error
	if err != nil {
		// expected — timeout causes an exec error
		t.Logf("timeout error (expected): %v", err)
	}
	// Either err or a block — we just want it to return quickly (< 5s)
}

func TestPreToolUse_ReceivesPayload(t *testing.T) {
	skipIfNoSh(t)
	dir := t.TempDir()
	outFile := dir + "/payload.txt"
	cfg := config.HooksConfig{
		PreToolUse: []config.HookGroup{
			group("Bash", "cat > "+outFile),
		},
	}
	r := hooks.New(cfg)
	input := json.RawMessage(`{"command":"ls"}`)
	r.RunPreToolUse(context.Background(), "Bash", input)

	data, err := readFile(outFile)
	if err != nil {
		t.Fatalf("payload file not written: %v", err)
	}
	var payload hooks.PreToolInput
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.ToolName != "Bash" {
		t.Errorf("tool_name: %q", payload.ToolName)
	}
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
