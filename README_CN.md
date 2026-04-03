# gocc

[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://golang.org)
[![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/hankwenyx/15c8b3d0509c2bfdf27684db18e6dc9c/raw)](TEST_REPORT.md)
[![License](https://img.shields.io/badge/License-Research_Only-blue)](#license)

基于 Claude 的交互式命令行软件工程 Agent。

> **⚠️ 重要声明**
>
> 本项目是一个**仅供研究用途的项目**，基于对 Claude Code 源代码的研究，通过分析公开发布的 NPM 包与 Source Map 还原而来。
>
> 本仓库并不代表 Anthropic 官方内部开发仓库的结构。
>
> 仅供研究与学习使用，禁止商业用途。

[English](README.md) | 中文

---

## 安装

```bash
# 通过 go install 安装
go install github.com/hankwenyx/claude-code-go/cmd/gocc@latest

# 或本地构建
git clone https://github.com/hankwenyx/claude-code-go
cd claude-code-go
make build        # 生成 ./bin/gocc
```

## 快速开始

```bash
export ANTHROPIC_API_KEY=your-key

gocc "解释一下这个代码库"              # headless 单轮
echo "main.go 做什么用的？" | gocc     # 管道输入
gocc -i                                # 交互式 TUI（在终端中自动启动）
```

---

## CLI 参数

```
gocc [flags] [message]
```

| 参数 | 短参 | 说明 |
|------|------|------|
| `--model` | `-m` | 使用的模型（默认：`claude-sonnet-4-6`） |
| `--max-tokens` | `-t` | 最大输出 token 数（默认：4096） |
| `--api-key` | `-k` | API Key（优先于环境变量和配置文件） |
| `--no-tools` | | 单轮纯文本模式，不使用工具 |
| `--allow` | | 为本次运行追加允许规则，如 `--allow "Bash(git *)"`（可重复） |
| `--bypass-permissions` | | 跳过所有权限检查 |
| `--permissive` | | 自动放行读取和安全命令；危险操作仍会弹出确认 |
| `--interactive` | `-i` | 强制进入 TUI 模式 |
| `--resume` | | 按 ID 恢复已保存的 TUI 会话 |
| `--brief` | | 聊天模式：隐藏工具调用，用 `SendUserMessage` 回复 |

**示例：**

```bash
gocc "你好"
gocc -m claude-opus-4-6 "review 这个 PR"
gocc --allow "Bash(go *)" "跑一下测试"
gocc --permissive -i                          # TUI，读取/安全命令预先放行
gocc --bypass-permissions "列出所有文件"       # 跳过所有权限提示
gocc -i --resume abc123def456                 # 恢复上一次的会话
```

---

## TUI

在 stdin 和 stdout 均为终端时自动启动（直接运行 `gocc` 即可），也可用 `-i` 强制进入。

### 快捷键

| 按键 | 功能 |
|------|------|
| `Enter` | 发送消息 |
| `Ctrl+Enter` | 插入换行（多行输入） |
| `↑` / `Ctrl+P` | 上一条历史输入 |
| `↓` / `Ctrl+N` | 下一条历史输入 |
| `PgUp` / `PgDn` | 滚动输出区 |
| `Ctrl+C` | 退出 |

### Slash 命令

| 命令 | 说明 |
|------|------|
| `/clear` | 清空对话历史和 token 计数 |
| `/compact` | 压缩对话以节省 token，同时将记忆保存到磁盘 |
| `/model [名称]` | 查看当前模型，或切换：`/model claude-opus-4-6` |
| `/help` | 显示快捷键和命令说明 |
| `/exit` | 退出 TUI |

### 边等待边输入（消息排队）

Agent 运行期间可以继续输入下一条消息，按 `Enter` 排队，状态栏显示：

```
Running…  ·  ⏎ 已排队: <预览>  Esc 撤回
```

可排队多条消息，当前轮结束后按顺序依次发送。按 `Esc` 撤回最后一条排队消息。

### `@file` 文件提及

在消息中内联文件内容：

```
@README.md
@./pkg/agent/agent.go
@/绝对路径/文件.txt
```

### 权限确认框

当 Agent 要执行未预先授权的命令时，状态栏弹出确认提示：

```
Allow Bash(rm -rf tmp)? [y]es / [n]o / [a]lways / [s]kip
```

| 按键 | 动作 |
|------|------|
| `y` | 本次允许 |
| `n` | 拒绝 |
| `a` | 本次会话内始终允许此模式 |
| `s` | 跳过（静默拒绝） |

---

## 配置

### API Key

按以下优先级查找，第一个非空值生效：

| 优先级 | 来源 |
|--------|------|
| 1 | `--api-key` 命令行参数 |
| 2 | 环境变量 `ANTHROPIC_AUTH_TOKEN` |
| 3 | 环境变量 `ANTHROPIC_API_KEY` |
| 4 | `settings.json` 的 `env.ANTHROPIC_AUTH_TOKEN` |
| 5 | `settings.json` 的 `env.ANTHROPIC_API_KEY` |
| 6 | `~/.claude/.credentials.json` 的 `apiKey` 字段 |

### 配置文件

四个层级按顺序合并，后面的层级覆盖前面的：

| 层级 | 路径 | 作用范围 |
|------|------|----------|
| 1 用户 | `~/.claude/settings.json` | 所有项目 |
| 2 项目 | `<cwd>/.claude/settings.json` | 提交到仓库，团队共享 |
| 3 本地 | `<cwd>/.claude/settings.local.json` | 不提交，个人私有覆盖 |
| 4 管理 | `/etc/claude-code/managed-settings.json`（Linux） | 组织策略，优先级最高 |

**`~/.claude/settings.json` 示例（个人本地配置）：**

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

| 优先级 | 来源 |
|--------|------|
| 1 | `--model` 命令行参数 |
| 2 | 合并后配置的 `env.ANTHROPIC_MODEL` |
| 3 | 合并后配置的 `model` 字段 |
| 4 | 默认值：`claude-sonnet-4-6` |

`model` 字段支持内联 token 上限后缀：

```
"model": "claude-opus-4-6[200k]"    → maxTokens = 200 000
"model": "claude-sonnet-4-6[1m]"    → maxTokens = 1 000 000
"model": "claude-haiku-4-5"          → maxTokens = 默认（4096）
```

### CLAUDE.md — 项目指令

CLAUDE.md 文件会注入到每次请求中。加载顺序（优先级从低到高）：

| 优先级 | 路径 |
|--------|------|
| 1 | `/etc/claude-code/CLAUDE.md` |
| 2 | `~/.claude/CLAUDE.md` |
| 3 | `~/.claude/rules/*.md` |
| 4 | `<git 根目录>/CLAUDE.md` … `<cwd>/CLAUDE.md`（从 cwd 向上查找） |
| 5 | `<dir>/.claude/CLAUDE.md` 和 `.claude/rules/*.md` |
| 6 | `<dir>/CLAUDE.local.md`（不提交） |
| 7 | `~/.claude/projects/<slug>/memory/MEMORY.md`（自动记忆） |

支持 `@include` 引入其他文件：

```markdown
@include ../shared/rules.md

# 项目规范
所有 Go error 必须使用 %w 包裹。
```

### 权限系统

**`defaultMode` 取值：**

| 模式 | 行为 |
|------|------|
| `default` | 非只读工具需要询问 |
| `auto` | CWD 内文件操作自动允许，其他询问 |
| `dontAsk` | 与 auto 类似，但从不询问 |
| `bypassPermissions` | 跳过所有检查，全部允许 |

在 headless / 非交互模式下，`ask` 一律视为 `deny`。

**规则语法：**

```
"Bash"             → 匹配任意 Bash 调用
"Bash(git *)"      → 命令以 "git " 开头
"Read(src/**)"     → Read 路径匹配 src/**
"Edit(*.go)"       → Edit 任意 .go 文件
```

**判断顺序**（第一条命中的规则生效）：

1. `deny` → 立即拒绝
2. `ask` → 询问用户（非交互模式则拒绝）
3. 只读工具 且 路径在 CWD / `additionalDirectories` 内 → 允许
4. 写文件工具 且 `auto`/`dontAsk` 模式 且 路径在 CWD 内 → 允许
5. `allow` → 允许
6. 默认 → 询问（非交互模式则拒绝）

### 使用第三方代理（如千帆 / Azure / Bedrock）

将 `ANTHROPIC_BASE_URL` 设为代理地址，`ANTHROPIC_MODEL` 设为代理侧的模型名称：

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

同时存在时，`ANTHROPIC_AUTH_TOKEN` 优先于 `ANTHROPIC_API_KEY`。

### 调试日志

```bash
CLAUDE_DEBUG=1 gocc "解释一下这段代码"
tail -f ~/.claude/logs/api_debug.log
```

日志包含完整的出站请求（messages、system prompt、tools）和每个流式事件。

---

## Hooks（钩子）

在每次工具调用前后执行自定义 Shell 命令。在 `settings.json` 的 `"hooks"` 键下配置：

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "echo '[hook] 即将执行 bash' >&2" }]
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
        "hooks": [{ "type": "command", "command": "osascript -e 'display notification \"完成\" with title \"gocc\"'" }]
      }
    ]
  }
}
```

| 事件 | 行为 |
|------|------|
| `PreToolUse` | 工具执行前运行。非零退出码阻断调用；stdout 可替换工具输入（JSON）。 |
| `PostToolUse` | 工具执行后运行。stdout 替换工具输出。 |
| `Notification` | 任务完成时触发，fire-and-forget。 |

**Matcher 语法：** `"Bash"` 精确匹配 · `"Bash(git *)"` 去参数后精确匹配 · `"File*"` 通配前缀 · `""` 或省略 → 匹配所有。

Hook 命令通过 stdin 接收 JSON payload，包含 `tool_name` 和 `input`（PostToolUse 还包含 `output` / `is_error`）。

---

## MCP（模型上下文协议）

连接外部 MCP 服务器以扩展工具。在 `settings.json` 中配置：

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

MCP 工具注册名为 `mcp__<serverName>__<toolName>`，与内置工具命名空间隔离。

支持传输方式：**stdio**（子进程 + JSON-RPC）和 **SSE**（HTTP）。

---

## 多 Agent

主 Agent 可通过 `Task` 工具派生子 Agent：

- **同步**：阻塞调用，完成后返回结果
- **异步**（`background: true`）：立即返回 `task_id`；完成后向主 Agent 注入 `<task-notification>`

子 Agent 使用独立的对话历史，拥有相同的工具和权限，不能再派生异步子任务。

**Todo 工具**（`TodoWrite` / `TodoRead`）：会话内共享的内存任务列表，用于追踪多步骤工作。

---

## 记忆系统

### `/compact` 与自动压缩

`/compact` 将当前对话压缩为单条摘要以释放上下文。同时提取关键事实，追加到：

```
~/.claude/projects/<cwd-slug>/memory/MEMORY.md
```

当会话 token 数超过 `CompactThreshold` 时自动触发压缩。

### MEMORY.md

每次新会话启动时自动注入同项目目录的 MEMORY.md，实现跨会话持久记忆。上限为 **200 行 / 25 KB**，超出时丢弃最早的内容。

---

## 与 Claude Code 的差异

本项目用 Go 重新实现了 Claude Code 的大部分核心功能，以下是主要差异。

### gocc 特有功能

| 功能 | 说明 |
|------|------|
| `--permissive` flag | 预批准读取和安全命令；危险操作仍弹出确认。Claude Code 需手动配置 `permissions`。 |
| `--allow` flag | 单次运行追加 allow 规则，无需修改 `settings.json`。 |
| `--bypass-permissions` flag | 按次跳过所有权限检查。Claude Code 只能通过 `settings.json` 配置。 |
| 内联 token 限制语法 | `"model": "claude-opus-4-6[200k]"` 在一个字段内同时指定模型和 maxTokens。 |
| 三方代理兼容 | 将 CLAUDE.md 合并进第一条 user message，规避部分代理不支持连续 user message 的限制。 |
| `/compact` 保存记忆 | compact 后额外调用 AutoMem 提取关键事实写入 `MEMORY.md`。 |
| 消息排队 | Agent 运行期间可排队多条消息，按顺序执行，Esc 撤回最后一条。 |

### 尚未实现的功能

| 功能 | 状态 |
|------|------|
| AutoDream（定期记忆整合） | 🔲 待做 |
| TodoV2（磁盘持久化、多 Agent 安全） | 🔲 待做 |
| 子 Agent Worktree 隔离 | 🔲 待做 |
| HTTP Hooks | 🔲 待做 |
| Extended Thinking（`ultrathink`） | 🔲 待做 |
| Skills（slash 命令扩展） | 🔲 待做 |
| Prompt cache 优化 | 🔲 待做 |
| 沙箱（bubblewrap） | 🔲 待做 |
| Web Browser 工具（Playwright） | 🔲 待做 |

---

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
    case agent.EventToolResult:
        // 工具调用完成
    case agent.EventError:
        fmt.Fprintln(os.Stderr, event.Error)
    }
}

// 同步 API
result, err := agent.RunAgentSync(ctx, "你好", opts)
```

---

## 开发

```bash
make build    # 编译 → ./bin/gocc
make test     # 运行所有测试
./bin/gocc "你好"
```

完整路线图见 [ROADMAP.md](ROADMAP.md)。

---

## License

**Research Only / 仅限研究用途**

## 声明

- Claude Code 原始源码版权归 Anthropic 所有
- 本仓库仅用于技术研究与学习
- 禁止商业用途
- 如有侵权，请联系删除
