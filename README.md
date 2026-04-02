# Claude Code Go

A Go implementation of [Claude Code](https://github.com/anthropics/claude-code).

> **⚠️ Important Notice / 重要声明**
>
> This is a **research-only project** based on the study of Claude Code source code, reconstructed through analysis of publicly released NPM packages and Source Maps.
>
> This repository does NOT represent the official internal development repository structure of Anthropic.
>
> For research and educational purposes only. Not for commercial use.

English | [中文](README_CN.md)

## Quick Start

```bash
export ANTHROPIC_API_KEY=your-key
go install github.com/hankwenyx/claude-code-go/cmd/claude@latest
claude "hello"
echo "what is this project?" | claude
```

## Library Usage

```go
import "github.com/hankwenyx/claude-code-go/pkg/agent"

opts := agent.AgentOptions{
    APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
    Model:     "claude-sonnet-4-6",
    MaxTokens: 4096,
}

// Streaming API
for event := range agent.RunAgent(ctx, "list .go files", opts) {
    switch event.Type {
    case agent.EventText:
        fmt.Print(event.Text)
    case agent.EventToolUse:
        fmt.Fprintf(os.Stderr, "[tool] %s\n", event.ToolCall.Name)
    }
}

// Sync API
result, err := agent.RunAgentSync(ctx, "hello", opts)
```

## Configuration

### API Key

API key is resolved in this order (first match wins):

| Priority | Source |
|----------|--------|
| 1 | `--api-key` flag |
| 2 | `ANTHROPIC_AUTH_TOKEN` environment variable |
| 3 | `ANTHROPIC_API_KEY` environment variable |
| 4 | `env.ANTHROPIC_AUTH_TOKEN` in `settings.json` |
| 5 | `env.ANTHROPIC_API_KEY` in `settings.json` |
| 6 | `~/.claude/.credentials.json` → `apiKey` field |

### Settings Files

Settings are loaded from four layers, merged in order (later overrides earlier):

| Layer | Path | Scope |
|-------|------|-------|
| 1 User | `~/.claude/settings.json` | all projects |
| 2 Project | `<cwd>/.claude/settings.json` | checked in, shared with team |
| 3 Local | `<cwd>/.claude/settings.local.json` | not checked in, personal overrides |
| 4 Managed | `/etc/claude-code/managed-settings.json` (Linux) | org policy, highest priority |

**`~/.claude/settings.json` example (typical local setup):**

```json
{
  "model": "claude-sonnet-4-6",
  "env": {
    "ANTHROPIC_API_KEY": "sk-ant-...",
    "ANTHROPIC_BASE_URL": "https://your-proxy.example.com",
    "ANTHROPIC_MODEL": "gpt-4o",
    "ANTHROPIC_CUSTOM_HEADERS": "X-Custom-Header:value,X-Another:val2"
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

**`<project>/.claude/settings.json` example (checked into repo):**

```json
{
  "model": "claude-opus-4-6[200k]",
  "permissions": {
    "allow": ["Bash(make *)", "Bash(go test *)"]
  }
}
```

### Model Selection

Model is resolved in this order:

| Priority | Source |
|----------|--------|
| 1 | `--model` flag |
| 2 | `env.ANTHROPIC_MODEL` in merged settings |
| 3 | `model` field in merged settings |
| 4 | Default: `claude-sonnet-4-6` |

The `model` field supports an inline token limit suffix:

```
"model": "claude-opus-4-6[200k]"   → model=claude-opus-4-6, maxTokens=200000
"model": "claude-sonnet-4-6[1m]"   → model=claude-sonnet-4-6, maxTokens=1000000
"model": "claude-haiku-4-5"         → model=claude-haiku-4-5, maxTokens=default
```

### CLAUDE.md — Project Instructions

CLAUDE.md files are loaded and injected into every request as context. Loading order (lowest → highest priority):

| Priority | Path | Type |
|----------|------|------|
| 1 | `/etc/claude-code/CLAUDE.md` | managed (org policy) |
| 2 | `~/.claude/CLAUDE.md` | user global |
| 3 | `~/.claude/rules/*.md` | user global rules |
| 4 | `<git-root>/CLAUDE.md` … `<cwd>/CLAUDE.md` | project (walks up from cwd) |
| 5 | `<dir>/.claude/CLAUDE.md` and `.claude/rules/*.md` | project per-dir |
| 6 | `<dir>/CLAUDE.local.md` | local (not checked in) |
| 7 | `~/.claude/projects/<slug>/memory/MEMORY.md` | auto-memory |

CLAUDE.md files support `@include` to pull in other files:

```markdown
@include ../shared/rules.md
@include .claude/security.md

# Project Rules
Always use Go error wrapping with %w.
```

### Permissions

Permissions control which tool calls are allowed. Rules use glob syntax.

**`defaultMode` values:**

| Mode | Behaviour |
|------|-----------|
| `default` | ask for non-read-only tools (interactive only) |
| `auto` | allow file ops inside CWD/additionalDirectories, ask otherwise |
| `dontAsk` | like auto but never asks — allows everything in allowed dirs |
| `bypassPermissions` | allow all (no checks) |

In headless / non-interactive mode (CLI `claude` command), `ask` always becomes `deny`.

**Rule syntax:**

```
"Bash"                 → match any Bash call
"Bash(git *)"          → Bash where command starts with "git "
"Bash(npm run:*)"      → Bash where command has prefix "npm run"
"Read(src/**)"         → Read with path matching src/**
"Edit(*.go)"           → Edit any .go file
```

**Evaluation order** (first match wins):

1. `deny` rules — immediately deny
2. `ask` rules — prompt user (or deny if non-interactive)
3. Read-only file tools inside CWD / `additionalDirectories` — allow
4. Write file tools in `auto`/`dontAsk` mode inside CWD — allow
5. `allow` rules — allow
6. Default — ask (or deny if non-interactive)

**Example: allow specific commands, deny dangerous ones:**

```json
{
  "permissions": {
    "defaultMode": "auto",
    "allow": [
      "Bash(git *)",
      "Bash(go *)",
      "Bash(make *)",
      "Bash(cat *)"
    ],
    "deny": [
      "Bash(rm *)",
      "Bash(sudo *)",
      "Bash(curl * | bash)"
    ],
    "ask": [
      "Bash(curl *)",
      "Bash(wget *)"
    ],
    "additionalDirectories": ["/tmp/scratch"]
  }
}
```

### Using a Third-Party Proxy (e.g. Qianfan / Azure / Bedrock)

Set `ANTHROPIC_BASE_URL` to the proxy endpoint and `ANTHROPIC_MODEL` to the model name the proxy expects:

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "your-proxy-token",
    "ANTHROPIC_BASE_URL":   "https://qianfan.baidubce.com/v2/ai_custom_endpoint/v1",
    "ANTHROPIC_MODEL":      "glm-4",
    "ANTHROPIC_CUSTOM_HEADERS": "X-Region:cn-north-1"
  }
}
```

Note: `ANTHROPIC_AUTH_TOKEN` takes priority over `ANTHROPIC_API_KEY` when both are present.

### Debug Logging

Set `CLAUDE_DEBUG=1` to write full request/response to `~/.claude/logs/api_debug.log`:

```bash
CLAUDE_DEBUG=1 claude "explain this code"
tail -f ~/.claude/logs/api_debug.log
```

The log includes the complete outgoing request (messages, system prompt, tools) and each streaming event, with thinking blocks logged in full when complete.

---

## Roadmap

### Phase 1 — Headless Core (Current)

> CLI + importable Go library, no TUI

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 1a | API Client (SSE streaming) + minimal agent loop (single-turn, no tools) | ✅ Done |
| 1b | settings.json 4-layer loading + CLAUDE.md loading with @include | ✅ Done |
| 1c | Tool interface + Bash/FileRead/FileEdit/FileWrite/Glob/Grep + permission system | ✅ Done |
| 1d | WebFetch + tool result truncation + concurrent execution (read-only parallel, write serial) | ✅ Done |
| 1e | SDK migration (anthropic-sdk-go v1.29.0) + disk persistence + credentials.json fallback + example_test.go | ✅ Done |

**Deliverables:**
- `pkg/` — importable Go library (`RunAgent` / `RunAgentSync`)
- `cmd/claude/` — CLI (stdin prompt, stdout result)
- Compatible with `~/.claude/settings.json` and `CLAUDE.md`

---

### Phase 2 — TUI (Terminal UI)

> Interactive terminal UI close to the original Claude Code

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 2a | Basic REPL: input box + output area (Markdown rendering) | 🔲 Todo |
| 2b | Tool call UI: collapsible tool name/args/result, spinner | 🔲 Todo |
| 2c | Permission dialog: yes/no/always/skip for ask-mode permissions | 🔲 Todo |
| 2d | Multi-line input, history (up/down), `@file` mention | 🔲 Todo |
| 2e | Slash commands: `/clear`, `/help`, `/exit`, `/compact`, `/config` | 🔲 Todo |
| 2f | Status bar (token usage, model name, CWD) | 🔲 Todo |
| 2g | Chat view mode (`SendUserMessage` tool, `--brief` flag) | 🔲 Todo |

**Tech:** [Bubbletea](https://github.com/charmbracelet/bubbletea) + Lipgloss + Glamour

---

### Phase 3 — MCP (Model Context Protocol)

> Connect to MCP servers for external tool registration

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 3a | MCP stdio transport: subprocess + JSON-RPC | 🔲 Todo |
| 3b | `tools/list` → dynamic tool registration | 🔲 Todo |
| 3c | `tools/call` → execution + result | 🔲 Todo |
| 3d | SSE transport (HTTP MCP server) | 🔲 Todo |
| 3e | settings.json `mcpServers` auto-connect | 🔲 Todo |
| 3f | MCP permission rules (`mcp__server__tool` format) | 🔲 Todo |

---

### Phase 4 — AgentTool (Multi-Agent)

> Main agent spawns sub-agents for parallel work

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 4a | Sync sub-agent: blocking `Agent` tool | 🔲 Todo |
| 4b | Async sub-agent: background goroutine, returns `task_id` | 🔲 Todo |
| 4c | Task notifications: inject `<task-notification>` to main agent | 🔲 Todo |
| 4d | 30s panel eviction after completion | 🔲 Todo |
| 4e | TodoV1: in-memory task list (`TodoWrite` tool) | 🔲 Todo |
| 4f | TodoV2: disk-persisted, dependencies, multi-agent safe | 🔲 Todo |
| 4g | Worktree isolation (`isolation: "worktree"`) | 🔲 Todo |

---

### Phase 5 — Memory System

> Cross-session persistent memory

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 5a | Session Memory: dialog compression at token threshold | 🔲 Todo |
| 5b | AutoMem: extract memories post-session → `~/.claude/projects/{slug}/memory/` | 🔲 Todo |
| 5c | AutoDream: periodic memory consolidation (4-gate + PID lock + rollback) | 🔲 Todo |
| 5d | MEMORY.md index: 200-line/25KB truncation, two-phase write | 🔲 Todo |

---

### Phase 6 — Hooks

> Pre/Post tool execution user-defined hooks

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 6a | `PreToolUse` hook: runs before tool, can block/modify input | 🔲 Todo |
| 6b | `PostToolUse` hook: runs after tool, can modify output | 🔲 Todo |
| 6c | `Notification` hook: triggered on task completion | 🔲 Todo |
| 6d | HTTP hooks (`allowedHttpHookUrls`) | 🔲 Todo |

---

### Phase 7 — Advanced Features (Optional)

| Feature | Priority |
|---------|----------|
| Dialog compression (`/compact`) + autocompact | High |
| Prompt cache optimization | High |
| Extended Thinking (`ultrathink`) | Medium |
| Skills (slash command extensions) | Medium |
| StatusLine plugin | Medium |
| Sandbox (bubblewrap, Linux only) | Low |
| Web Browser tool (Playwright) | Low |

---

## Dependency Graph

```
Phase 1 (Headless Core)
    │
    ├──────────────────────┬──────────────────┐
    ▼                      ▼                  ▼
Phase 2 (TUI)         Phase 3 (MCP)     Phase 6 (Hooks)
    │                      │                  │
    └───────────┬───────────┘                  │
                ▼                              │
            Phase 4 (AgentTool) ◄─────────────┘
                │
                ▼
            Phase 5 (Memory)
                │
                ▼
            Phase 7 (Advanced, independent)
```

## Documentation

- [Architecture Overview](docs/architecture.md)
- [Agent Loop](docs/agent-loop.md)
- [Context & Prompt](docs/context-and-prompt.md)
- [Memory System](docs/memory-system.md)
- [Tool System](docs/tool-system.md)
- [Task & Multi-Agent](docs/task-and-multi-agent.md)
- [Skills & Permissions](docs/skill-and-permissions.md)

## Development

```bash
make build   # build binary
make test    # run tests
./bin/claude "hello"
```

## License

**Research Only**

---

## Disclaimer / 声明

### English

- **Source Code Ownership**: All original Claude Code source code is copyrighted by Anthropic.
- **Purpose**: This repository is for technical research and learning purposes only.
- **Commercial Use Prohibited**: Do not use this project for commercial purposes.
- **Removal Request**: If there is any infringement, please contact us for removal.

### 中文

- **源码版权**：Claude Code 原始源码版权归 Anthropic 所有
- **用途**：本仓库仅用于技术研究与学习
- **禁止商业**：请勿用于商业用途
- **侵权处理**：如有侵权，请联系删除
