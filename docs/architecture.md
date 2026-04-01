# Claude Code 架构文档

> 本文是整体架构的 high-level 概览。每个子系统的深入细节请参阅 `docs/` 目录下的对应文档。

---

## 技术栈

| 层次 | 选型 |
|------|------|
| 运行时 | Bun >= 1.3.5 |
| 语言 | TypeScript + React JSX |
| 终端 UI | Ink（基于 React 的 CLI UI 框架） |
| 构建 | Bun bundle，`feature()` 条件编译 + 死代码消除 |
| 模块系统 | ESM |
| AI | Anthropic Claude API、Agent SDK |
| 外部协议 | MCP（Model Context Protocol）、LSP、OAuth |

---

## 系统整体架构

```
┌──────────────────────────────────────────────────────────────────┐
│                         Claude Code CLI                           │
│                                                                   │
│   输入层                核心层               服务层               │
│  ┌─────────┐          ┌──────────────┐    ┌─────────────────┐   │
│  │PromptInput│────────▶│  Agent Loop  │───▶│   MCP Service   │   │
│  │ 斜杠命令  │          │  (query.ts)  │    │   LSP Service   │   │
│  └─────────┘          └──────┬───────┘    │   Compact Svc   │   │
│                               │            │   Analytics     │   │
│   工具层                      ▼            └─────────────────┘   │
│  ┌─────────────────┐   ┌──────────────┐                         │
│  │   Tool System   │◀──│  Claude API  │    状态层               │
│  │  Bash/Read/Edit │   │  (streaming) │   ┌─────────────────┐   │
│  │  Agent/MCP/...  │   └──────────────┘   │  bootstrap/     │   │
│  └─────────────────┘                      │  state.ts       │   │
│                                            │  (全局单例)      │   │
│   记忆层                                   └─────────────────┘   │
│  ┌─────────────────┐                                             │
│  │  CLAUDE.md      │   任务层                                    │
│  │  AutoMem        │  ┌─────────────────┐                       │
│  │  SessionMemory  │  │  Task System    │                       │
│  │  TeamMem        │  │  Multi-Agent    │                       │
│  └─────────────────┘  └─────────────────┘                       │
└──────────────────────────────────────────────────────────────────┘

外部依赖：
  Anthropic API  ←─  核心 AI 能力
  MCP Servers    ←─  外部工具扩展
  LSP Servers    ←─  语言诊断
  claude.ai      ←─  Bridge 远程控制
```

---

## 启动流程

```
bun run dev
    │
    ▼
src/dev-entry.ts           检查所有 import 是否存在（源码重建安全网）
    │
    ▼
src/entrypoints/cli.tsx    快速路由（按参数分发到不同入口，懒加载）
    │ 大多数情况
    ▼
src/main.tsx               完整 CLI 初始化：
    ├─ 加载配置（config + GrowthBook feature flags）
    ├─ 连接 MCP 服务器
    ├─ 注册工具 / 命令 / 技能 / 插件
    └─ 启动 REPL（交互界面）
```

`cli.tsx` 的快速路由让特殊模式（daemon/bridge/bg-sessions 等）无需加载完整 CLI，冷启动更快。

---

## 核心子系统

### Agent Loop — 核心对话循环

每次用户输入触发一个循环：调用模型 → 执行工具 → 继续循环，直到任务完成或用户中断。

详见 → [agent-loop.md](agent-loop.md)

### 上下文管理与 Prompt 系统

决定"每次 API 调用发什么内容"：系统提示词构建、Token 预算分配、对话历史压缩、Prompt Cache 策略。

详见 → [context-and-prompt.md](context-and-prompt.md)

### 记忆系统

六个层级的持久化记忆，从企业策略到个人会话笔记，AI 可以自动提取和整理跨会话记忆。

详见 → [memory-system.md](memory-system.md)

### 工具系统

Claude 通过工具与外部世界交互：文件读写、Shell 执行、网络请求、MCP 代理等，有统一的权限检查和并发调度机制。

详见 → [tool-system.md](tool-system.md)

### 任务系统与多 Agent

支持并行启动多个子 Agent，通过 Inbox 通信，共享 TodoV2 任务列表，支持 worktree 隔离和远程执行。

详见 → [task-and-multi-agent.md](task-and-multi-agent.md)

### 技能与权限系统

技能是可复用的 AI 工作流（Markdown 文件），权限系统控制 AI 的每个操作是否需要确认。

详见 → [skill-and-permissions.md](skill-and-permissions.md)

---

## 数据流：一次完整的工具调用

```
你输入一条消息
        │
        ▼
消息 + 系统提示词 + 工具定义 ──► Claude API（流式）
        │
        ▼
模型返回 tool_use
        │
  ┌─────┴──────┐
  │ 权限检查   │ ← 弹对话框 or 匹配规则
  └─────┬──────┘
        │ 通过
        ▼
  工具执行（Read / Bash / Edit...）
        │
        ▼
  结果追加到消息历史 ──► 继续下一轮 API 调用
```

---

## 关键配置文件

```
~/.claude/settings.json          全局用户配置（权限规则、模型、MCP 等）
.claude/settings.json            项目配置（提交到 git）
.claude/settings.local.json      本地覆盖（gitignore）
~/.claude/CLAUDE.md              全局指令记忆
CLAUDE.md                        项目指令记忆
~/.claude/projects/.../memory/   AutoMem 跨会话记忆
```

---

## Feature Flag 体系

`feature('FLAG_NAME')` 在构建时进行死代码消除，运行时通过 GrowthBook 动态控制。主要 Flag：

| Flag | 功能 |
|------|------|
| `DAEMON` | 后台守护进程 |
| `BRIDGE_MODE` | claude.ai 远程控制 |
| `KAIROS` | 助手模式（完整功能集） |
| `AGENT_TRIGGERS` | 定时 Agent（Cron） |
| `WORKFLOW_SCRIPTS` | 工作流脚本 |
| `WEB_BROWSER_TOOL` | 完整浏览器控制 |
| `HISTORY_SNIP` | 对话历史剪辑 |
| `CONTEXT_COLLAPSE` | 上下文折叠 |
| `ULTRAPLAN` | 超级计划模式 |

---

## 关键依赖

| 依赖 | 用途 |
|------|------|
| `@anthropic-ai/sdk` | Claude API 核心客户端 |
| `@anthropic-ai/claude-agent-sdk` | Agent SDK |
| `@modelcontextprotocol/sdk` | MCP 协议 |
| `ink` | CLI React 渲染引擎 |
| `commander` | CLI 参数解析 |
| `zod` | 工具参数 Schema 验证 |
| `@growthbook/growthbook` | Feature flags / A/B 测试 |
| `@opentelemetry/*` | 遥测 |
| `execa` | 子进程执行 |
