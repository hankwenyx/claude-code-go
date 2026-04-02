# Claude Code Go

[Claude Code](https://github.com/anthropics/claude-code) 的 Go 语言实现。

> **⚠️ 重要声明**
>
> 这是一个**仅限研究用途**的项目，基于 Claude Code 源码研究，通过公开发布的 NPM 包与 Source Map 分析还原。
>
> 本仓库**不代表** Anthropic 官方内部开发仓库结构。
>
> 仅供研究与学习使用，请勿用于商业用途。

[English](README.md) | 中文

## 快速开始

```bash
export ANTHROPIC_API_KEY=your-key
go install github.com/hankwenyx/claude-code-go/cmd/claude@latest
claude "你好"
echo "这个项目是做什么的？" | claude
```

## 作为库使用

```go
import "github.com/hankwenyx/claude-code-go/pkg/agent"

opts := agent.AgentOptions{
    APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
    Model:     "claude-sonnet-4-6",
    MaxTokens: 4096,
}

// 流式 API
for event := range agent.RunAgent(ctx, "列出 .go 文件", opts) {
    switch event.Type {
    case agent.EventText:
        fmt.Print(event.Text)
    case agent.EventToolUse:
        fmt.Fprintf(os.Stderr, "[tool] %s\n", event.ToolCall.Name)
    }
}

// 同步 API
result, err := agent.RunAgentSync(ctx, "你好", opts)
```

## 配置

### API Key

按以下优先级依次查找，第一个非空值生效：

| 优先级 | 来源 |
|--------|------|
| 1 | `--api-key` 命令行参数 |
| 2 | 环境变量 `ANTHROPIC_AUTH_TOKEN` |
| 3 | 环境变量 `ANTHROPIC_API_KEY` |
| 4 | `settings.json` 的 `env.ANTHROPIC_AUTH_TOKEN` |
| 5 | `settings.json` 的 `env.ANTHROPIC_API_KEY` |
| 6 | `~/.claude/.credentials.json` 的 `apiKey` 字段 |

### 配置文件

配置从四个层级加载并合并，后面的层级覆盖前面的：

| 层级 | 路径 | 作用范围 |
|------|------|----------|
| 1 用户 | `~/.claude/settings.json` | 所有项目生效 |
| 2 项目 | `<cwd>/.claude/settings.json` | 提交到仓库，团队共享 |
| 3 本地 | `<cwd>/.claude/settings.local.json` | 不提交，个人私有覆盖 |
| 4 管理 | `/etc/claude-code/managed-settings.json`（Linux）| 组织策略，优先级最高 |

**`~/.claude/settings.json` 示例（个人本地配置）：**

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

**`<project>/.claude/settings.json` 示例（提交到仓库）：**

```json
{
  "model": "claude-opus-4-6[200k]",
  "permissions": {
    "allow": ["Bash(make *)", "Bash(go test *)"]
  }
}
```

### 模型选择

按以下优先级选取模型，第一个非空值生效：

| 优先级 | 来源 |
|--------|------|
| 1 | `--model` 命令行参数 |
| 2 | 合并后配置的 `env.ANTHROPIC_MODEL` |
| 3 | 合并后配置的 `model` 字段 |
| 4 | 默认值：`claude-sonnet-4-6` |

`model` 字段支持内联 token 上限后缀：

```
"model": "claude-opus-4-6[200k]"   → model=claude-opus-4-6，maxTokens=200000
"model": "claude-sonnet-4-6[1m]"   → model=claude-sonnet-4-6，maxTokens=1000000
"model": "claude-haiku-4-5"         → model=claude-haiku-4-5，使用默认 token 上限
```

### CLAUDE.md — 项目指令

CLAUDE.md 文件会在每次请求时作为上下文注入。加载顺序（优先级从低到高）：

| 优先级 | 路径 | 类型 |
|--------|------|------|
| 1 | `/etc/claude-code/CLAUDE.md` | 管理员策略 |
| 2 | `~/.claude/CLAUDE.md` | 用户全局 |
| 3 | `~/.claude/rules/*.md` | 用户全局规则 |
| 4 | `<git 根目录>/CLAUDE.md` … `<cwd>/CLAUDE.md` | 项目（从 cwd 向上查找） |
| 5 | `<dir>/.claude/CLAUDE.md` 和 `.claude/rules/*.md` | 各目录项目配置 |
| 6 | `<dir>/CLAUDE.local.md` | 本地私有（不提交） |
| 7 | `~/.claude/projects/<slug>/memory/MEMORY.md` | 自动记忆 |

CLAUDE.md 支持 `@include` 引入其他文件：

```markdown
@include ../shared/rules.md
@include .claude/security.md

# 项目规范
所有 Go error 必须使用 %w 包裹。
```

### 权限系统

权限控制哪些工具调用被允许执行，规则使用 glob 语法。

**`defaultMode` 取值：**

| 模式 | 行为 |
|------|------|
| `default` | 非只读工具需要询问（仅交互模式） |
| `auto` | CWD / additionalDirectories 内的文件操作自动允许，其他询问 |
| `dontAsk` | 与 auto 类似，但在允许目录内从不询问 |
| `bypassPermissions` | 跳过所有权限检查，全部允许 |

在 headless / 非交互模式（`claude` 命令行）下，`ask` 一律视为 `deny`。

**规则语法：**

```
"Bash"                → 匹配任意 Bash 调用
"Bash(git *)"         → Bash 命令以 "git " 开头
"Bash(npm run:*)"     → Bash 命令以 "npm run" 为前缀
"Read(src/**)"        → Read 路径匹配 src/**
"Edit(*.go)"          → Edit 任意 .go 文件
```

**判断顺序**（第一条命中的规则生效）：

1. `deny` 规则 — 立即拒绝
2. `ask` 规则 — 询问用户（非交互模式则拒绝）
3. 只读文件工具 且 路径在 CWD / `additionalDirectories` 内 — 允许
4. 写文件工具 且 `auto`/`dontAsk` 模式 且 路径在 CWD 内 — 允许
5. `allow` 规则 — 允许
6. 默认 — 询问（非交互模式则拒绝）

**示例：允许常用命令，拒绝危险操作：**

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

### 使用第三方代理（如千帆 / Azure / Bedrock）

将 `ANTHROPIC_BASE_URL` 设为代理地址，`ANTHROPIC_MODEL` 设为代理侧的模型名称：

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

注意：同时存在时，`ANTHROPIC_AUTH_TOKEN` 优先于 `ANTHROPIC_API_KEY`。

### 调试日志

设置 `CLAUDE_DEBUG=1` 可将完整的请求 / 响应写入 `~/.claude/logs/api_debug.log`：

```bash
CLAUDE_DEBUG=1 claude "解释一下这段代码"
tail -f ~/.claude/logs/api_debug.log
```

日志包含完整的出站请求（messages、system prompt、tools）和每个流式事件，thinking 块会在完整接收后一次性写出。

---

## 路线图

### Phase 1 — Headless 核心（已完成）

> CLI + 可导入 Go 库，无 TUI

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 1a | API Client（SSE 流式）+ 最简 agent loop（单轮，无工具） | ✅ 完成 |
| 1b | settings.json 四层加载 + CLAUDE.md 加载（含 @include） | ✅ 完成 |
| 1c | Tool 接口 + Bash/FileRead/FileEdit/FileWrite/Glob/Grep + 权限系统 | ✅ 完成 |
| 1d | WebFetch + 工具结果截断 + 并发执行（只读并行，写串行） | ✅ 完成 |
| 1e | SDK 迁移（anthropic-sdk-go v1.29.0）+ 磁盘持久化 + credentials.json 回退 + example_test.go | ✅ 完成 |

**交付物：**
- `pkg/` — 可导入 Go 库（`RunAgent` / `RunAgentSync`）
- `cmd/claude/` — CLI（stdin 输入，stdout 输出）
- 兼容 `~/.claude/settings.json` 和 `CLAUDE.md`

---

### Phase 2 — TUI（终端交互界面）

> 接近原版 Claude Code 的交互式终端 UI

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 2a | 基础 REPL：输入框 + 输出区（Markdown 渲染） | 🔲 待做 |
| 2b | 工具调用 UI：可折叠的工具名/参数/结果，spinner | 🔲 待做 |
| 2c | 权限对话框：ask 模式下 yes/no/always/skip | 🔲 待做 |
| 2d | 多行输入、历史记录（上下方向键）、`@file` 提及 | 🔲 待做 |
| 2e | Slash 命令：`/clear`、`/help`、`/exit`、`/compact`、`/config` | 🔲 待做 |
| 2f | 状态栏（token 用量、模型名、CWD） | 🔲 待做 |
| 2g | 聊天视图模式（`SendUserMessage` 工具，`--brief` 参数） | 🔲 待做 |

**技术栈：** [Bubbletea](https://github.com/charmbracelet/bubbletea) + Lipgloss + Glamour

---

### Phase 3 — MCP（模型上下文协议）

> 连接 MCP 服务器，动态注册外部工具

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 3a | MCP stdio 传输：子进程 + JSON-RPC | 🔲 待做 |
| 3b | `tools/list` → 动态工具注册 | 🔲 待做 |
| 3c | `tools/call` → 执行 + 返回结果 | 🔲 待做 |
| 3d | SSE 传输（HTTP MCP 服务器） | 🔲 待做 |
| 3e | settings.json `mcpServers` 自动连接 | 🔲 待做 |
| 3f | MCP 权限规则（`mcp__server__tool` 格式） | 🔲 待做 |

---

### Phase 4 — AgentTool（多 Agent）

> 主 Agent 派生子 Agent 并行工作

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 4a | 同步子 Agent：阻塞式 `Agent` 工具 | 🔲 待做 |
| 4b | 异步子 Agent：后台 goroutine，返回 `task_id` | 🔲 待做 |
| 4c | 任务通知：向主 Agent 注入 `<task-notification>` | 🔲 待做 |
| 4d | 完成后 30s 面板自动销毁 | 🔲 待做 |
| 4e | TodoV1：内存任务列表（`TodoWrite` 工具） | 🔲 待做 |
| 4f | TodoV2：磁盘持久化、依赖关系、多 Agent 安全 | 🔲 待做 |
| 4g | Worktree 隔离（`isolation: "worktree"`） | 🔲 待做 |

---

### Phase 5 — 记忆系统

> 跨会话持久记忆

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 5a | Session Memory：达到 token 阈值时压缩对话 | 🔲 待做 |
| 5b | AutoMem：会话结束后提取记忆 → `~/.claude/projects/{slug}/memory/` | 🔲 待做 |
| 5c | AutoDream：定期记忆整合（4 门控 + PID 锁 + 回滚） | 🔲 待做 |
| 5d | MEMORY.md 索引：200 行 / 25KB 截断，两阶段写入 | 🔲 待做 |

---

### Phase 6 — Hooks

> 工具执行前后的用户自定义钩子

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 6a | `PreToolUse` hook：工具执行前运行，可阻断或修改输入 | 🔲 待做 |
| 6b | `PostToolUse` hook：工具执行后运行，可修改输出 | 🔲 待做 |
| 6c | `Notification` hook：任务完成时触发 | 🔲 待做 |
| 6d | HTTP hooks（`allowedHttpHookUrls`） | 🔲 待做 |

---

### Phase 7 — 高级功能（可选）

| 功能 | 优先级 |
|------|--------|
| 对话压缩（`/compact`）+ 自动压缩 | 高 |
| Prompt cache 优化 | 高 |
| Extended Thinking（`ultrathink`） | 中 |
| Skills（slash 命令扩展） | 中 |
| StatusLine 插件 | 中 |
| 沙箱（bubblewrap，仅 Linux） | 低 |
| Web Browser 工具（Playwright） | 低 |

---

## 依赖关系图

```
Phase 1（Headless 核心）
    │
    ├──────────────────────┬──────────────────┐
    ▼                      ▼                  ▼
Phase 2（TUI）        Phase 3（MCP）    Phase 6（Hooks）
    │                      │                  │
    └───────────┬───────────┘                  │
                ▼                              │
          Phase 4（AgentTool）◄────────────────┘
                │
                ▼
          Phase 5（记忆系统）
                │
                ▼
          Phase 7（高级功能，独立）
```

## 文档

- [架构概览](docs/architecture.md)
- [Agent Loop](docs/agent-loop.md)
- [Context & Prompt](docs/context-and-prompt.md)
- [记忆系统](docs/memory-system.md)
- [工具系统](docs/tool-system.md)
- [任务与多 Agent](docs/task-and-multi-agent.md)
- [Skills 与权限](docs/skill-and-permissions.md)

## 开发

```bash
make build   # 编译二进制
make test    # 运行测试
./bin/claude "你好"
```

## License

**Research Only / 仅限研究用途**

---

## 声明

- **源码版权**：Claude Code 原始源码版权归 Anthropic 所有
- **用途**：本仓库仅用于技术研究与学习
- **禁止商业**：请勿用于商业用途
- **侵权处理**：如有侵权，请联系删除

---

## Disclaimer

- **Source Code Ownership**: All original Claude Code source code is copyrighted by Anthropic.
- **Purpose**: This repository is for technical research and learning purposes only.
- **Commercial Use Prohibited**: Do not use this project for commercial purposes.
- **Removal Request**: If there is any infringement, please contact us for removal.
