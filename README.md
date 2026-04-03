# gocc

[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://golang.org)
[![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/hankwenyx/15c8b3d0509c2bfdf27684db18e6dc9c/raw)](TEST_REPORT.md)
[![License](https://img.shields.io/badge/License-Research_Only-blue)](#license)

An interactive CLI agent for software engineering tasks, powered by Claude.

> **⚠️ Important Notice / 重要声明**
>
> This is a **research-only project** based on the study of Claude Code source code, reconstructed through analysis of publicly released NPM packages and Source Maps.
>
> This repository does NOT represent the official internal development repository structure of Anthropic.
>
> For research and educational purposes only. Not for commercial use.

English | [中文](README_CN.md)

---

## Install

```bash
# Via go install
go install github.com/hankwenyx/claude-code-go/cmd/gocc@latest

# Or build locally
git clone https://github.com/hankwenyx/claude-code-go
cd claude-code-go
make build        # produces ./bin/gocc
```

## Quick Start

```bash
export ANTHROPIC_API_KEY=your-key

gocc "explain this codebase"          # headless, single turn
echo "what does main.go do?" | gocc   # pipe input
gocc -i                                # interactive TUI (auto-launches in a terminal)
```

---

## CLI Reference

```
gocc [flags] [message]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--model` | `-m` | Model to use (default: `claude-sonnet-4-6`) |
| `--max-tokens` | `-t` | Maximum output tokens (default: 4096) |
| `--api-key` | `-k` | API key (overrides env and settings) |
| `--no-tools` | | Single-turn text only, no tools |
| `--allow` | | Add a permission allow rule for this run, e.g. `--allow "Bash(git *)"` (repeatable) |
| `--bypass-permissions` | | Skip all permission checks |
| `--permissive` | | Pre-approve reads and safe commands; still ask for destructive ops |
| `--interactive` | `-i` | Force TUI mode |
| `--resume` | | Resume a saved TUI session by ID |
| `--brief` | | Chat mode: hide tool calls, use `SendUserMessage` for replies |

**Examples:**

```bash
gocc "hello"
gocc -m claude-opus-4-6 "review this PR"
gocc --allow "Bash(go *)" "run the tests"
gocc --permissive -i                          # TUI, reads/safe commands pre-approved
gocc --bypass-permissions "list all files"    # skip all prompts
gocc -i --resume abc123def456                 # resume a previous session
```

---

## TUI

The TUI launches automatically when stdin and stdout are both terminals (just run `gocc`). Force it with `-i`.

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Ctrl+Enter` | Insert newline (multi-line input) |
| `↑` / `Ctrl+P` | Previous message in history |
| `↓` / `Ctrl+N` | Next message in history |
| `PgUp` / `PgDn` | Scroll output |
| `Ctrl+C` | Quit |

### Slash Commands

| Command | Description |
|---------|-------------|
| `/clear` | Clear conversation history and token counter |
| `/compact` | Summarise conversation to save tokens; also saves memories to disk |
| `/model [name]` | Show current model, or switch: `/model claude-opus-4-6` |
| `/help` | Show keyboard shortcuts and command reference |
| `/exit` | Quit TUI |

### Queuing Messages While Agent Runs

You can type the next message while the agent is still working. Press `Enter` to queue it — the status bar shows:

```
Running…  ·  ⏎ queued: <preview>  Esc to cancel
```

Multiple messages can be queued and are sent in order after each turn completes. Press `Esc` to cancel the last queued message.

### `@file` Mentions

Inline a file's contents into your message:

```
@README.md
@./pkg/agent/agent.go
@/absolute/path/to/file.txt
```

### Permission Dialog

When the agent wants to run a command that hasn't been pre-approved, a prompt appears:

```
Allow Bash(rm -rf tmp)? [y]es / [n]o / [a]lways / [s]kip
```

| Key | Action |
|-----|--------|
| `y` | Allow this call once |
| `n` | Deny |
| `a` | Always allow this pattern for the rest of the session |
| `s` | Skip (deny silently) |

---

## Configuration

### API Key

Resolved in this order (first match wins):

| Priority | Source |
|----------|--------|
| 1 | `--api-key` flag |
| 2 | `ANTHROPIC_AUTH_TOKEN` env var |
| 3 | `ANTHROPIC_API_KEY` env var |
| 4 | `env.ANTHROPIC_AUTH_TOKEN` in `settings.json` |
| 5 | `env.ANTHROPIC_API_KEY` in `settings.json` |
| 6 | `~/.claude/.credentials.json` → `apiKey` |

### Settings Files

Four layers merged in order — later layers override earlier ones:

| Layer | Path | Scope |
|-------|------|-------|
| 1 User | `~/.claude/settings.json` | all projects |
| 2 Project | `<cwd>/.claude/settings.json` | checked in, team-shared |
| 3 Local | `<cwd>/.claude/settings.local.json` | personal overrides, not checked in |
| 4 Managed | `/etc/claude-code/managed-settings.json` (Linux) | org policy, highest priority |

**`~/.claude/settings.json` — typical personal setup:**

```json
{
  "model": "claude-sonnet-4-6",
  "env": {
    "ANTHROPIC_API_KEY": "sk-ant-...",
    "ANTHROPIC_BASE_URL": "https://your-proxy.example.com",
    "ANTHROPIC_MODEL": "claude-sonnet-4-6",
    "ANTHROPIC_CUSTOM_HEADERS": "X-Custom-Header:value"
  },
  "permissions": {
    "defaultMode": "auto",
    "allow": ["Bash(git *)", "Bash(go *)"],
    "deny": ["Bash(rm *)"],
    "ask": ["Bash(curl *)"],
    "additionalDirectories": ["/tmp/workspace"]
  }
}
```

**`<project>/.claude/settings.json` — checked into repo:**

```json
{
  "model": "claude-opus-4-6[200k]",
  "permissions": {
    "allow": ["Bash(make *)", "Bash(go test *)"]
  }
}
```

### Model Selection

| Priority | Source |
|----------|--------|
| 1 | `--model` flag |
| 2 | `env.ANTHROPIC_MODEL` in merged settings |
| 3 | `model` field in merged settings |
| 4 | Default: `claude-sonnet-4-6` |

Inline token limit in `model` field:

```
"model": "claude-opus-4-6[200k]"    → maxTokens = 200 000
"model": "claude-sonnet-4-6[1m]"    → maxTokens = 1 000 000
"model": "claude-haiku-4-5"          → maxTokens = default (4096)
```

### CLAUDE.md — Project Instructions

CLAUDE.md files are injected into every request. Loading order (lowest → highest priority):

| Priority | Path |
|----------|------|
| 1 | `/etc/claude-code/CLAUDE.md` |
| 2 | `~/.claude/CLAUDE.md` |
| 3 | `~/.claude/rules/*.md` |
| 4 | `<git-root>/CLAUDE.md` … `<cwd>/CLAUDE.md` (walks up from cwd) |
| 5 | `<dir>/.claude/CLAUDE.md` and `.claude/rules/*.md` |
| 6 | `<dir>/CLAUDE.local.md` (not checked in) |
| 7 | `~/.claude/projects/<slug>/memory/MEMORY.md` (auto-memory) |

Supports `@include`:

```markdown
@include ../shared/rules.md

# Project Rules
Always wrap Go errors with %w.
```

### Permissions

**`defaultMode` values:**

| Mode | Behaviour |
|------|-----------|
| `default` | ask for all non-read-only tools |
| `auto` | auto-allow file ops inside CWD; ask otherwise |
| `dontAsk` | like `auto` but never asks |
| `bypassPermissions` | allow everything |

In headless / non-interactive mode, `ask` rules always resolve to `deny`.

**Rule syntax:**

```
"Bash"             → any Bash call
"Bash(git *)"      → command starts with "git "
"Read(src/**)"     → Read path matches src/**
"Edit(*.go)"       → Edit any .go file
```

**Evaluation order** (first match wins):

1. `deny` → deny immediately
2. `ask` → prompt user (deny if non-interactive)
3. Read-only tool inside CWD / `additionalDirectories` → allow
4. Write tool, `auto`/`dontAsk` mode, inside CWD → allow
5. `allow` → allow
6. Default → ask (deny if non-interactive)

### Using a Third-Party Proxy

Set `ANTHROPIC_BASE_URL` to your proxy and `ANTHROPIC_MODEL` to the model name the proxy expects:

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "your-token",
    "ANTHROPIC_BASE_URL": "https://qianfan.baidubce.com/v2/ai_custom_endpoint/v1",
    "ANTHROPIC_MODEL": "ernie-4.5",
    "ANTHROPIC_CUSTOM_HEADERS": "X-Region:cn-north-1"
  }
}
```

`ANTHROPIC_AUTH_TOKEN` takes priority over `ANTHROPIC_API_KEY` when both are set.

### Debug Logging

```bash
CLAUDE_DEBUG=1 gocc "explain this code"
tail -f ~/.claude/logs/api_debug.log
```

Logs every outgoing request (messages, system prompt, tools) and every streaming event.

---

## Hooks

Run shell commands before/after each tool call. Configure in `settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "echo '[hook] bash about to run' >&2" }]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Edit",
        "hooks": [{ "type": "command", "command": "gofmt -w \"$(echo $HOOK_INPUT | jq -r .file_path)\"" }]
      }
    ],
    "Notification": [
      {
        "hooks": [{ "type": "command", "command": "osascript -e 'display notification \"Done\" with title \"gocc\"'" }]
      }
    ]
  }
}
```

| Event | Behaviour |
|-------|-----------|
| `PreToolUse` | Runs before the tool. Non-zero exit blocks the call. Stdout can replace the tool input (JSON). |
| `PostToolUse` | Runs after the tool. Stdout replaces the tool output. |
| `Notification` | Fire-and-forget on task completion. |

**Matcher syntax:** `"Bash"` exact · `"Bash(git *)"` strip-params then exact · `"File*"` prefix wildcard · `""` / omit → match all.

Hook commands receive a JSON payload on stdin with `tool_name` and `input` (plus `output` / `is_error` for PostToolUse).

---

## MCP (Model Context Protocol)

Connect external MCP servers to add tools. Configure in `settings.json`:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    },
    "my-http-server": {
      "url": "http://localhost:3000/sse"
    }
  },
  "permissions": {
    "allow": ["mcp__filesystem__read_file"],
    "deny":  ["mcp__filesystem__write_file"]
  }
}
```

MCP tools are registered as `mcp__<serverName>__<toolName>`, keeping them separate from built-in tools.

Supported transports: **stdio** (subprocess + JSON-RPC) and **SSE** (HTTP).

---

## Multi-Agent

The agent can spawn sub-agents via the `Task` tool:

- **Sync**: blocking call, waits for result
- **Async** (`background: true`): returns `task_id` immediately; injects `<task-notification>` into the main agent's next turn

Sub-agents share the same tools and permissions but start with a fresh conversation. They cannot spawn further async tasks.

**Todo tools** (`TodoWrite` / `TodoRead`): in-memory task list for tracking multi-step work within a session.

---

## Memory

### `/compact` and Auto-Compaction

`/compact` summarises the current conversation into a single block to free up context. It also extracts key facts and appends them to:

```
~/.claude/projects/<cwd-slug>/memory/MEMORY.md
```

Auto-compaction triggers automatically when the session token count exceeds `CompactThreshold`.

### MEMORY.md

Injected at the start of every new session for the same project directory, giving the agent persistent memory across sessions. Capped at **200 lines / 25 KB** — oldest entries are dropped first when the limit is reached.

---

## Differences from Claude Code

This project reimplements most of Claude Code's core features in Go. Notable differences:

### gocc-specific features

| Feature | Description |
|---------|-------------|
| `--permissive` flag | Pre-approves reads and safe commands; still prompts for destructive ops. Claude Code requires manual `permissions` config. |
| `--allow` flag | Adds a permission rule per-run without editing `settings.json`. |
| `--bypass-permissions` flag | Per-run flag to skip all checks. Claude Code only supports this via `settings.json`. |
| Inline token limit | `"model": "claude-opus-4-6[200k]"` sets model and maxTokens in one field. |
| Third-party proxy compatibility | Merges CLAUDE.md into the first user message to work around proxies that reject consecutive `user` messages. |
| `/compact` saves memories | `compact` also runs AutoMem to extract key facts and persist them to `MEMORY.md`. |
| Type-ahead message queue | Queue multiple messages during a running turn; Esc cancels the last one. |

### Not yet implemented

| Feature | Status |
|---------|--------|
| AutoDream (periodic memory consolidation) | 🔲 Todo |
| TodoV2 (disk-persisted, multi-agent safe) | 🔲 Todo |
| Worktree isolation for sub-agents | 🔲 Todo |
| HTTP hooks | 🔲 Todo |
| Extended Thinking (`ultrathink`) | 🔲 Todo |
| Skills (slash command extensions) | 🔲 Todo |
| Prompt cache optimization | 🔲 Todo |
| Sandbox (bubblewrap) | 🔲 Todo |
| Web Browser tool (Playwright) | 🔲 Todo |

---

## Library Usage

```go
import "github.com/hankwenyx/claude-code-go/pkg/agent"

opts := agent.AgentOptions{
    APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
    Model:     "claude-sonnet-4-6",
    MaxTokens: 4096,
}

// Streaming
for event := range agent.RunAgent(ctx, "list .go files", opts) {
    switch event.Type {
    case agent.EventText:
        fmt.Print(event.Text)
    case agent.EventToolUse:
        fmt.Fprintf(os.Stderr, "[tool] %s\n", event.ToolCall.Name)
    case agent.EventToolResult:
        // tool finished
    case agent.EventError:
        fmt.Fprintln(os.Stderr, event.Error)
    }
}

// Blocking
result, err := agent.RunAgentSync(ctx, "hello", opts)
```

---

## Development

```bash
make build    # build → ./bin/gocc
make test     # run all tests
./bin/gocc "hello"
```

See [ROADMAP.md](ROADMAP.md) for the full implementation plan.

---

## License

**Research Only**

## Disclaimer

- Source code of Claude Code is copyright Anthropic.
- This repository is for technical research and learning only.
- Commercial use is prohibited.
- Contact us for removal if there is any infringement.
