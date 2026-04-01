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
  └─ DreamTaskState      记忆整合任务 [KAIROS]
```

所有任务共享基础状态：`id`、`type`、`status`（pending/running/completed/failed/killed）、`startTime`、`endTime`

---

## LocalAgentTask 完整状态结构

```typescript
type LocalAgentTaskState = {
  // ── 基础字段 ──
  id: string                    // 唯一任务 ID
  type: 'local_agent'
  status: TaskStatus
  startTime: number
  endTime?: number

  // ── Agent 运行状态 ──
  agentId: string               // 对应 runAgent 中的 agentId
  isBackgrounded: boolean       // 是否已后台化
  pendingMessages: Message[]    // 等待注入到 Agent 的消息队列（SendMessageTool 写入）
  retain: boolean               // true = evictTerminalTask 不自动删除

  // ── 持久化 ──
  diskLoaded: boolean           // 是否从磁盘 transcript 恢复的
  evictAfter: number | undefined  // 超时驱逐时间戳（Date.now() + PANEL_GRACE_MS）

  // ── 输出 ──
  toolUseId: string | undefined  // 触发此 Agent 的 tool_use block ID
  summary: string               // 完成时的摘要文字
  outputPath: string | undefined // 结果输出文件路径
}
```

`PANEL_GRACE_MS = 30_000`（30 秒）：Agent 进入终态后，UI 面板保留显示 30 秒再从 AppState 移除。

---

## 子 Agent 的完整生命周期

### 注册路径：registerAsyncAgent vs 前台

```
AgentTool.call()
  │
  ├─ shouldRunAsync = false → runAgent() 直接运行（阻塞当前 tool_use）
  │   └─ 返回时：结果文本直接写入 tool_result
  │
  └─ shouldRunAsync = true → registerAsyncAgent()
      ├─ 生成 taskId，在 AppState 创建 LocalAgentTaskState（status: pending）
      ├─ 立刻返回 task_id 给模型（非阻塞）
      └─ 在后台异步执行 runAgent()
          └─ 完成时：enqueueAgentNotification() 向父 Agent 注入通知
```

### runAgent 的 20 步初始化流程（`src/tools/AgentTool/runAgent.ts`）

```
1.  参数解包          分离 agentDefinition、toolWhitelist、systemPrompt 等
2.  agentId 生成      crypto.randomUUID()（用于 transcript/Perfetto/工具隔离）
3.  Perfetto tracing  registerPerfettoAgent(agentId)（追踪此 Agent 的性能轨迹）
4.  工具解析          resolveAgentTools()：展开 allowedTools 白名单
5.  权限模式设置      根据 agentDefinition.permissionMode 覆盖继承的模式
6.  上下文克隆        创建 forkContextMessages（克隆消息历史，保留 cache prefix）
7.  用户消息构建      构建初始 user 消息（prompt + attachments + context）
8.  系统提示词构建    getAgentSystemPrompt()：继承父 system + env 详情追加
9.  AbortController   async Agent：全新独立 AbortController；sync：共享父 abortController
10. SubagentStart hooks  executeSubagentStartHooks()，收集 additionalContexts
11. frontmatter hooks   registerFrontmatterHooks()（if agentDefinition.hooks 存在）
12. Skill 预加载      resolveSkillName() + 并发加载，追加为 user 消息
13. MCP 初始化        initializeAgentMcpServers()：inline 定义 = 新建+清理；string 引用 = 共享父连接
14. 工具去重合并      uniqBy([...resolvedTools, ...agentMcpTools], 'name')
15. agentOptions 构建 isNonInteractiveSession: isAsync, thinkingConfig（除非 useExactTools）
16. Subagent 上下文创建  createSubagentContext()：agentId + 隔离 readFileState + 消息历史
17. 缓存安全参数回调  onCacheSafeParams()（如有）：传 systemPrompt/userContext 供 fork 摘要
18. Transcript 持久化  recordSidechainTranscript()（fire-and-forget）
19. metadata 写入     writeAgentMetadata(agentId, { agentType, worktreePath?, description? })
20. query() 启动      进入主循环（与主 Agent 使用完全相同的 queryLoop 机制）
```

### finally 块的 10 步清理

```
1.  await mcpCleanup()                 断开 inline（新建的）Agent 专属 MCP 服务器
2.  clearSessionHooks(rootSetAppState, agentId)  注销 frontmatter hooks
3.  cleanupAgentTracking(agentId)       清理 Prompt Cache 断裂检测状态
4.  agentToolUseContext.readFileState.clear()  释放文件状态缓存
5.  initialMessages.length = 0         释放克隆的上下文消息数组
6.  unregisterPerfettoAgent(agentId)   从 Perfetto 追踪注册表移除
7.  clearAgentTranscriptSubdir(agentId) 清理 transcript 子目录映射
8.  AppState.todos 清理                移除此 Agent 的 todo 条目（防鲸会话内存泄漏）
9.  killShellTasksForAgent(agentId)    终止此 Agent 派生的所有后台 Bash（防 PPID=1 僵尸）
10. killMonitorMcpTasksForAgent()      终止 MonitorMcp 任务（feature: MONITOR_TOOL）
```

---

## LocalAgentTask vs RemoteAgentTask

| | LocalAgentTask | RemoteAgentTask |
|--|----------------|-----------------|
| **在哪里运行** | 本地进程内 | Anthropic 云端 CCR |
| **通信方式** | AppState 直接更新 | HTTP 轮询远端 API |
| **怎么中断** | `AbortController.abort()` | `archiveRemoteSession()` |
| **怎么恢复** | 读本地 sidechain transcript | 读 RemoteAgentMetadata sidecar |
| **能弹权限对话框吗** | 可以（async 模式自动拒绝）| 不行（远端无 UI）|
| **适合什么场景** | 常规子任务 | ultraplan / ultrareview 等大任务 |

**RemoteAgentTask 轮询常量**：

```typescript
POLL_INTERVAL_MS      = 1000   // 每 1 秒轮询一次远端状态
STABLE_IDLE_POLLS     = 5      // 连续 5 次 idle 才认为稳定完成
REMOTE_REVIEW_TIMEOUT_MS = 30 * 60 * 1000  // 30 分钟等待超时
```

---

## Worktree 隔离

**命名规则**（`src/utils/worktree.ts`）

```
agentId 取前 8 位 → slug: agent-${agentId.slice(0, 8)}
worktree 路径：   .claude/worktrees/<slug>/
git 分支：        worktree-<slug>
```

**创建流程**

```
createWorktree(slug, baseBranch)
  │
  ├─ git worktree add .claude/worktrees/<slug> -b worktree-<slug>
  ├─ 大目录符号链接（opt-in：settings.worktree.symlinkDirectories 配置的目录列表）
  │   symlinkDirectories() 创建 'dir' 类型符号链接，跳过 ENOENT/EEXIST
  ├─ 复制 .claude/settings.local.json 等私人配置
  └─ writeAgentMetadata(agentId, { worktreePath })
```

**stale 清理规则**

仅清理匹配以下 `EPHEMERAL_WORKTREE_PATTERNS` 之一的 worktree（且无未提交变更、无未推送 commit）：

```typescript
const EPHEMERAL_WORKTREE_PATTERNS = [
  /^agent-a[0-9a-f]{7}$/,                            // AgentTool agent worktrees
  /^wf_[0-9a-f]{8}-[0-9a-f]{3}-\d+$/,               // WorkflowTool 当前格式
  /^wf-\d+$/,                                        // WorkflowTool 旧格式
  /^bridge-[A-Za-z0-9_]+(-[A-Za-z0-9_]+)*$/,        // bridgeMain
  /^job-[a-zA-Z0-9._-]{1,55}-[0-9a-f]{8}$/,         // template job worktrees
]
// 用户通过 EnterWorktree 手动命名的 worktree 不会被自动清理
```

---

## 团队协作（Teammate 系统）

### Teammate 的三种执行后端

```
in-process  → 同一进程，AsyncLocalStorage 隔离上下文
              轻量、速度快、支持 UI 交互

tmux        → 独立进程，在 tmux 分屏里显示
              可视化、进程隔离、支持 macOS/Linux

iterm2      → iTerm2 原生分屏（macOS 专属）
```

### Inbox/Mailbox 机制（`src/utils/teammateMailbox.ts`）

```
消息存储：~/.claude/teams/<team>/inboxes/<agentName>.json
文件锁（proper-lockfile）：
  LOCK_OPTIONS = {
    retries: { retries: 10, minTimeout: 5, maxTimeout: 100 }
  }
```

**结构化协议消息**（`isStructuredProtocolMessage()` 门控）：

| 消息类型 | 用途 |
|---------|------|
| `task_assignment` | Leader 给 Worker 分配任务 |
| `idle_notification` | Worker 告诉 Leader "我空了" |
| `plan_approval_request/response` | Leader 审批 Worker 的计划 |
| `permission_request/response` | Worker 向 Leader 申请权限 |
| `shutdown_request/response` | 优雅关机协议 |
| `context_update` | 向 Agent 注入额外上下文 |

---

## Todo 任务管理

### TodoV1（TodoWriteTool）— 简单任务清单

```
存储：内存（AppState.todos[agentId]）
操作：整个列表一次性替换
特点：简单、轻量、不持久化
适合：单 Agent 的临时任务清单
```

### TodoV2（TaskCreate/Update/List/Get）— 多 Agent 协作任务

```
存储：~/.claude/tasks/<taskListId>/<taskId>.json
操作：按 task ID 增删改查
特点：持久化、依赖关系、多 Agent 感知
```

**并发锁**：列表级写入使用 list-level lock；单个任务读写使用 per-task lock（防 concurrent claim 竞态）。

**`claimTask` 阻塞检测**：

```typescript
// TaskUpdate 将 status 设为 in_progress 时
const task = loadTask(taskId)
if (task.blockedBy.length > 0) {
  const blockers = task.blockedBy.filter(id => loadTask(id)?.status !== 'completed')
  if (blockers.length > 0) {
    return { error: `Task blocked by: ${blockers.join(', ')}` }
  }
}
```

**`deleteTask` 级联清理**：

删除任务时自动清理依赖引用：
1. 遍历所有 `blocks` 中的任务 → 从其 `blockedBy` 列表中移除被删除的 taskId
2. 遍历所有 `blockedBy` 中的任务 → 从其 `blocks` 列表中移除

**`notifyTasksUpdated()`**：通过 `EventEmitter` 在进程内信号广播，`TaskList` UI 订阅该信号实时刷新。

---

## 后台任务通知 XML 格式

子 Agent 完成时，通过 `enqueueAgentNotification()` 向父 Agent 注入通知：

```xml
<task-notification>
<task-id>{taskId}</task-id>
<tool-use-id>{toolUseId}</tool-use-id>           <!-- 可选，有 toolUseId 时附带 -->
<output-file>{outputPath}</output-file>           <!-- 结果文件路径 -->
<status>completed|failed|killed</status>
<summary>{summary text}</summary>
<result>                                          <!-- 完成时附带 -->
{final result text}
</result>
<usage>                                           <!-- Token 使用统计（如有）-->
input_tokens: X, output_tokens: Y
</usage>
<worktree>                                        <!-- worktree 模式时附带 -->
{worktree path}
</worktree>
</task-notification>
```

父 Agent 在下一轮循环中看到通知，自主决定后续行动。

**30 秒驱逐**：`evictAfter = Date.now() + PANEL_GRACE_MS`（`PANEL_GRACE_MS = 30_000`）——完成后 UI 面板显示 30 秒，之后从 AppState 移除（防内存泄漏）。

---

## 权限隔离：子 Agent 不会"越权"

```
父 Agent 已批准了高危权限（如 Bash(rm -rf)）
        │
        └─ 子 Agent 会继承吗？答：不会！

子 Agent 只有显式分配的 allowedTools 白名单
父 Agent 运行时已批准的 session 级规则不传递给子 Agent
```

特殊情况：
- `isAsync=true`（后台运行）→ 自动 `isNonInteractiveSession=true`（不弹权限对话框）
- `bubble` 模式 → 允许异步 Agent 把权限请求冒泡给父 Agent 处理

---

## 关键文件

| 文件 | 里面有什么 |
|------|-----------|
| `src/tasks/types.ts` | 所有任务类型定义（LocalAgentTaskState 完整字段）|
| `src/utils/task/framework.ts` | 任务注册、更新、轮询、驱逐（PANEL_GRACE_MS）|
| `src/tasks/LocalAgentTask/LocalAgentTask.tsx` | 本地异步子 Agent 状态机（enqueueAgentNotification）|
| `src/tasks/RemoteAgentTask/` | 远程 Agent 轮询（POLL_INTERVAL_MS/STABLE_IDLE_POLLS/REMOTE_REVIEW_TIMEOUT_MS）|
| `src/tools/AgentTool/runAgent.ts` | 20 步初始化 + 10 步 finally 清理 |
| `src/tools/AgentTool/AgentTool.tsx` | 路由决策（shouldRunAsync 6 条件、assistantForceAsync）|
| `src/utils/worktree.ts` | Git worktree 创建（slug/路径/分支命名、stale 清理）|
| `src/utils/teammateMailbox.ts` | 文件锁 inbox（LOCK_OPTIONS、协议消息类型）|
| `src/tools/TaskCreateTool/` | TodoV2 任务创建（磁盘路径、锁、级联清理）|
| `src/tools/TodoWriteTool/` | TodoV1 整表替换（内存 AppState）|
| `src/utils/swarm/spawnInProcess.ts` | 创建 in-process Teammate |
| `src/utils/swarm/inProcessRunner.ts` | Teammate 执行循环 |
| `src/tools/SendMessageTool/` | Agent 间消息路由 |
| `src/tools/TeamCreateTool/` | 团队创建（TeamFile + TaskList 初始化）|
