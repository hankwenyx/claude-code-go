# 任务系统与多 Agent 架构

Claude Code 支持启动多个 AI Agent 并行工作。这篇文档介绍任务（Task）的生命周期、多 Agent 的协作方式，以及 Todo 任务管理系统。

---

## 为什么需要多 Agent？

单个 Agent 是线性的：一步一步地做事。遇到可以并行的任务（比如同时对 20 个文件做代码审查），单 Agent 效率很低。

多 Agent 架构让"主 Agent"可以派出多个"子 Agent"并行工作：

```
主 Agent（你在对话的那个）
  │
  ├─ 派出子 Agent A → 负责"分析 auth 模块"
  ├─ 派出子 Agent B → 负责"分析 api 模块"
  └─ 派出子 Agent C → 负责"分析 db 模块"
           │
           └─ A、B、C 同时工作（三倍速度）
                    │
                    └─ 完成后把结果报告给主 Agent
```

---

## 任务类型总览

```
TaskState（所有任务的类型）
  │
  ├─ LocalAgentTask      本地异步子 Agent（最常用）
  ├─ RemoteAgentTask     远程云端 Agent（CCR 环境）
  ├─ LocalShellTask      后台 Bash 命令
  ├─ InProcessTeammateTask 同进程内的团队成员 Agent
  ├─ LocalWorkflowTask   工作流任务 [WORKFLOW_SCRIPTS]
  ├─ MonitorMcpTask      MCP 监控任务
  └─ DreamTaskState      梦境任务 [KAIROS]
```

所有任务共享基础状态：`id`、`type`、`status`（pending/running/completed/failed/killed）、`startTime`、`endTime`

---

## 子 Agent 的完整生命周期

### 创建（AgentTool 被调用时）

```
AgentTool.call({
  prompt: "分析这个模块的安全性",
  description: "安全分析",
  subagent_type: "Explore",
  run_in_background: true
})
        │
        ▼
① 查找 Agent 定义（Explore / Plan / General-Purpose 等）
② 组装工具列表（父 Agent 工具的受限子集）
③ 检查 MCP 依赖（等待 pending 服务器最多 30s）
④ 注册任务到 AppState.tasks[taskId]
⑤ 根据模式选择执行方式 ──────────────────────────────────┐
                                                          │
        ┌─────────────────────────────────────────────────┘
        │
        ├─ 同步前台 → runAgent() 阻塞当前工具调用直到完成
        ├─ 异步后台 → registerAsyncAgent()，立刻返回 task_id
        ├─ worktree 隔离 → 先建 git worktree，在里面运行
        └─ 远程执行 → teleport 到 CCR 环境运行
```

### 运行（runAgent() 核心）

```
runAgent(params)
        │
① 生成唯一 agentId
② 初始化私有 MCP 连接（合并父连接 + Agent 私有）
③ 设置权限模式（根据 agentDefinition.permissionMode）
④ 克隆 ToolUseContext（独立的文件状态缓存、AbortController）
⑤ 执行 SubagentStart Hooks
⑥ 进入 query() 主循环（与主 Agent 使用同一套机制）
        │
        └─ 每条消息实时写入 sidechain transcript：
           ~/.claude/projects/<project>/subagents/<agentId>.jsonl
```

### 销毁（finally 块中）

```
无论成功失败，runAgent 退出时：
  ├─ mcpCleanup()           清理私有 MCP 连接
  ├─ clearSessionHooks()    注销钩子
  ├─ readFileState.clear()  释放文件状态缓存
  ├─ killShellTasksForAgent()  终止派生的所有后台 Bash
  └─ evictTerminalTask()    30s 后从 AppState 移除此任务
```

---

## LocalAgentTask vs RemoteAgentTask

| | LocalAgentTask | RemoteAgentTask |
|--|----------------|-----------------|
| **在哪里运行** | 本地进程内 | Anthropic 云端 CCR |
| **通信方式** | AppState 直接更新 | HTTP 轮询远端 API |
| **怎么中断** | `AbortController.abort()` | `archiveRemoteSession()` |
| **怎么恢复** | 读本地 sidechain transcript | 读 RemoteAgentMetadata sidecar |
| **能弹权限对话框吗** | 可以（异步模式自动拒绝） | 不行（远端无 UI） |
| **适合什么场景** | 常规子任务 | ultraplan / ultrareview 等大任务 |

---

## Worktree 隔离

当 Agent 需要独立修改代码，又不能影响主仓库时，可以用 worktree 隔离：

```
主仓库 (main branch)
  │
  └─ 创建 git worktree
       路径：.claude/worktrees/<slug>/
       分支：claude/<sessionId>/<slug>
              │
              ├─ 大目录（node_modules）用符号链接，避免磁盘爆炸
              ├─ 复制 .claude/settings.local.json 等配置
              └─ Agent 在隔离目录内工作，修改不影响主分支
```

**使用场景**：
- `AgentTool` 的 `isolation: 'worktree'` 参数
- `EnterWorktreeTool` 让整个 session 进入 worktree

**退出时**：检查是否有未提交的变更。有变更需要显式 `discard_changes: true` 才能强制清理。

---

## 团队协作（Teammate 系统）

多个 Agent 需要分工合作时，使用 Team 机制。

### 创建团队

```
TeamCreateTool 调用
  │
  ├─ 创建 TeamFile：~/.claude/teams/<teamName>/team.json
  │   记录：leadAgentId, members[], leadSessionId
  ├─ 初始化 TaskList（TodoV2 任务目录）
  └─ 注册清理钩子（session 退出时自动清理团队）
```

### Teammate 的三种执行后端

```
in-process  → 同一进程，AsyncLocalStorage 隔离上下文
              轻量、速度快、支持 UI 交互

tmux        → 独立进程，在 tmux 分屏里显示
              可视化、进程隔离、支持 macOS/Linux

iterm2      → iTerm2 原生分屏（macOS 专属）
```

### Agent 间如何通信？

通过 `SendMessageTool` 发消息，路由逻辑：

```
发送目标是谁？
  │
  ├─ bridge:<session-id>  → 跨机器传输（需用户确认）
  ├─ uds:<socket-path>    → Unix Domain Socket 本地传输
  ├─ LocalAgentTask 中的 Agent：
  │    ├─ 还在运行中 → 排队等下次工具轮次注入
  │    └─ 已停止   → 恢复并传入消息
  ├─ name@team 格式的 Teammate → 写入文件 inbox
  └─ * （广播） → 遍历所有团队成员，逐个写 inbox
```

### Inbox 机制

团队成员之间通过文件系统的 inbox 传递消息：

```
消息写入：~/.claude/teams/<team>/inboxes/<agentName>.json
使用文件锁（proper-lockfile）防止并发写入冲突
消息带 read 标志，分已读/未读
```

内置消息协议：

| 消息类型 | 用途 |
|---------|------|
| `task_assignment` | Leader 给 Worker 分配任务 |
| `idle_notification` | Worker 告诉 Leader "我空了" |
| `plan_approval_response` | Leader 审批 Worker 的计划 |
| `permission_request/response` | Worker 向 Leader 申请权限 |
| `shutdown_request/response` | 优雅关机协议 |

---

## Todo 任务管理

Claude Code 有两套任务管理系统（V1 和 V2），功能有显著差异。

### TodoV1（TodoWriteTool）— 简单任务清单

```
存储：内存（AppState.todos[agentId]）
操作：整个列表一次性替换
特点：简单、轻量、不持久化
适合：单 Agent 的临时任务清单

格式：
[
  { id: "1", content: "读取配置文件", status: "completed" },
  { id: "2", content: "分析安全漏洞", status: "in_progress" },
  { id: "3", content: "生成报告",     status: "pending" }
]
```

### TodoV2（TaskCreate/Update/List/Get）— 多 Agent 协作任务

```
存储：磁盘（~/.claude/teams/<teamName>/tasks/<taskId>.json）
操作：按 task ID 增删改查
特点：持久化、支持依赖关系、多 Agent 感知
适合：团队协作、复杂多步骤任务
```

TodoV2 的任务结构：

```json
{
  "id": "task-uuid",
  "subject": "分析认证模块",
  "description": "检查 JWT 实现是否有安全漏洞",
  "status": "in_progress",
  "owner": "security-agent@my-team",
  "blocks": ["task-report-uuid"],
  "blockedBy": [],
  "metadata": {}
}
```

任务状态流转：

```
pending → in_progress → completed
                     ↘ deleted（物理删除文件）
```

**多 Agent 感知**：设置 `status: in_progress` 时自动记录 `owner`；完成任务时通过 mailbox 通知被 blocks 的任务的 owner；`TaskList` 展示整个团队的任务进度。

### V1 vs V2 对比

| | TodoV1 | TodoV2 |
|--|--------|--------|
| **存储** | 内存 | 磁盘 |
| **持久化** | ✗（会话结束清空） | ✓ |
| **操作粒度** | 整表替换 | 单任务增删改查 |
| **依赖关系** | ✗ | ✓（blocks/blockedBy）|
| **多 Agent** | ✗ | ✓（owner、通知）|
| **适用场景** | 单 Agent 临时清单 | 团队复杂任务编排 |

---

## 权限隔离：子 Agent 不会"越权"

这是多 Agent 架构最重要的安全设计：

```
父 Agent 已批准了一个高危权限（如 Bash(rm -rf)）
        │
        └─ 子 Agent 会自动继承吗？
              │
              答：不会！

子 Agent 只有显式分配的工具白名单（allowedTools）
父 Agent 运行时已批准的 session 级规则，不传递给子 Agent
```

特殊情况：
- `isAsync=true`（后台运行）→ 自动设置"不弹权限对话框"（后台没有 UI）
- `bubble` 模式 → 允许异步 Agent 把权限请求冒泡给父 Agent 处理

---

## 后台任务如何通知父 Agent？

```
子 Agent 完成任务
  │
  ▼
生成通知消息（XML 格式）并追加到父 Agent 的消息队列：
  <task_notification>
    <task_id>xxx</task_id>
    <tool_use_id>yyy</tool_use_id>
    <task_type>local_agent</task_type>
    <status>completed</status>
    <summary>Task "分析认证模块" completed successfully</summary>
  </task_notification>
        │
        ▼
父 Agent 在下一轮看到通知，决定接下来怎么做

30 秒后（PANEL_GRACE_MS）：
        └─ 任务从 AppState 中移除（不再占内存）
```

---

## 关键文件

| 文件 | 里面有什么 |
|------|-----------|
| `src/tasks/types.ts` | 所有任务类型定义 |
| `src/utils/task/framework.ts` | 任务注册、更新、轮询、驱逐 |
| `src/tasks/LocalAgentTask/` | 本地异步子 Agent 状态机 |
| `src/tasks/RemoteAgentTask/` | 远程 Agent 轮询和状态 |
| `src/tasks/InProcessTeammateTask/` | 同进程 Teammate 生命周期 |
| `src/tools/AgentTool/runAgent.ts` | 子 Agent 执行环境初始化 |
| `src/tools/AgentTool/AgentTool.tsx` | 路由决策（同步/异步/worktree/远程） |
| `src/utils/swarm/spawnInProcess.ts` | 创建 in-process Teammate |
| `src/utils/swarm/inProcessRunner.ts` | Teammate 执行循环 |
| `src/utils/teammateMailbox.ts` | 文件锁 inbox 系统 |
| `src/tools/TeamCreateTool/` | 团队创建 |
| `src/tools/SendMessageTool/` | Agent 间消息路由 |
| `src/tools/TaskCreateTool/` | TodoV2 任务创建 |
| `src/tools/TodoWriteTool/` | TodoV1 整表替换 |
| `src/utils/worktree.ts` | Git worktree 创建和管理 |
