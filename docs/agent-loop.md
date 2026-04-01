# Agent Loop — 核心对话循环

Agent Loop 是 Claude Code 的"大脑主循环"。每当你输入一条消息，它负责完成从"接收输入"到"输出结果"的全部工作：调用模型、执行工具、处理错误、决定是否继续。

---

## 一句话理解

> **用户说话 → 模型思考 → 调用工具 → 回来继续思考 → 直到任务完成**

这个过程不是一次性的，而是一个循环（Loop）。模型可能调用十几次工具才完成一个任务，每次调用工具的结果都会反馈回来，让模型继续决策。

---

## 整体调用链

```
你的输入
  │
  ▼
handlePromptSubmit()          ← 处理提交，识别斜杠命令
  │
  ▼
processUserInput()            ← 规范化输入，构造消息
  │
  ▼
query()                       ← 公开入口，收集工具调用记录
  │                             完成后通知所有 UUID: notifyCommandLifecycle(uuid, 'completed')
  ▼
queryLoop()  ◄── 真正的主循环（while true）
  │              持有全部跨迭代可变状态（State 类型）
  ▼
deps.callModel()              ← 调用 Claude API（流式 SSE）
  │
  ▼
anthropic.beta.messages.create({ stream: true })
```

---

## 核心循环图解

可以把 `queryLoop()` 想象成一台"思考机器"，它每轮做这几件事：

```
┌─────────────────────────────────────────────────────────────────────┐
│                         queryLoop()                                 │
│                                                                     │
│  ┌──────────┐    ┌──────────────┐    ┌──────────────────────────┐   │
│  │ 准备消息  │───▶│  调用模型     │───▶│   模型返回了什么？          │   │
│  │ 截断/压缩 │    │  流式 SSE     │    │                          │   │
│  └──────────┘    └──────────────┘    └──────┬───────────────────┘   │
│                                             │                       │
│                               ┌────────────┴─────────────┐          │
│                               │                          │          │
│                         只是文字回复                  包含工具调用      │
│                               │                          │          │
│                               ▼                          ▼          │
│                          检查 Stop Hooks           执行工具           │
│                               │                    （可并发）         │
│                               │                          │          │
│                           任务完成 ◄─────────────── 结果追加到消息      │
│                                                          │          │
│                                                    继续下一轮  ───────┘
└─────────────────────────────────────────────────────────────────────┘
```

---

## 循环内部的跨迭代状态（State 类型）

`queryLoop()` 用 `State` 类型封装全部跨迭代可变状态，每次 `continue` 时整体重新赋值（不逐字段写）：

```typescript
type State = {
  messages: Message[]                               // 消息历史
  toolUseContext: ToolUseContext                    // 工具执行上下文
  autoCompactTracking: AutoCompactTrackingState     // 自动压缩跟踪
  maxOutputTokensRecoveryCount: number              // max_tokens 恢复计数，上限 3
  hasAttemptedReactiveCompact: boolean              // 是否已尝试响应式压缩
  maxOutputTokensOverride: number | undefined       // 升级后的 max_tokens 值
  pendingToolUseSummary: Promise<...> | undefined   // 工具摘要异步预取
  stopHookActive: boolean | undefined               // stop hook 是否激活
  turnCount: number                                 // 当前轮次计数
  transition: Continue | undefined                  // 上一次的继续原因
}
```

---

## 每一轮都在做什么（详细版）

### 第 1 步：准备要发给模型的消息

```
原始消息历史
  │
  ├─ 1. getMessagesAfterCompactBoundary：只取上次压缩边界之后的消息
  ├─ 2. applyToolResultBudget：超大工具结果截断（MAX 200K 字符/消息）
  ├─ 3. snip（HISTORY_SNIP）：历史剪辑
  ├─ 4. microcompact：删除重复工具结果（time-based 或 cached 路径）
  ├─ 5. context collapse（CONTEXT_COLLAPSE）：折叠旧上下文
  └─ 6. autocompact 检查：token 超过阈值 → 触发 AI 压缩
```

### 第 2 步：流式调用模型

Claude API 是流式返回的（SSE），每个 content block 完成时立刻 yield 给上层。
关键点：**不是等整个回复完成才处理，而是边接收边处理。**

```
流式事件流：
  message_start       → 初始化消息对象
  content_block_start → 开始一个内容块（文字 / 工具调用 / 思维链）
  content_block_delta → 增量内容到来（文字追加、JSON 参数追加）
  content_block_stop  → 本块完成 → 立即 yield 给上层处理
  message_delta       → 写回最终 usage / stop_reason（直接修改对象引用）
  message_stop        → 整条消息结束
```

一个特别的优化：**工具可以在模型还没说完话时就开始执行！**

```
模型正在 streaming... （还没说完）
  ├─ tool_use block A 完成 → 立刻开始执行 A
  ├─ tool_use block B 完成 → 立刻开始执行 B（和 A 并发）
  └─ 模型说完 → 消费剩余工具结果
```

这就是 `StreamingToolExecutor` 做的事情，它让工具执行和模型输出互相重叠，节省等待时间。

### 第 3 步：决定接下来怎么办

模型的回复分两种情况：

**情况 A：只有文字，没有工具调用**
```
没有工具 → 检查是否有错误（413 / max_tokens）→ 运行 Stop Hooks → 任务完成
```

**情况 B：包含工具调用**
```
有工具调用 → 执行工具 → 把结果追加到消息历史 → 继续下一轮循环
```

---

## StreamingToolExecutor — 并发调度核心

### 数据结构

每个工具被封装为 `TrackedTool`，拥有独立状态：

```typescript
type TrackedTool = {
  id: string
  status: 'queued' | 'executing' | 'completed' | 'yielded'
  isConcurrencySafe: boolean
  promise?: Promise<void>
  results?: Message[]
  pendingProgress: Message[]      // 立即 yield 的进度消息
  contextModifiers?: Array<...>
}
```

### 并发控制规则（`canExecuteTool`）

```
可以执行 = （没有任何工具在 executing）
          OR（新工具是 concurrencySafe 且所有 executing 工具也是 concurrencySafe）

isConcurrencySafe 由 toolDefinition.isConcurrencySafe(parsedInput) 决定
  → 只读工具（Read/Grep/Glob/WebFetch）返回 true
  → Bash/Edit/Write 返回 false（写操作串行）
```

### 两级 AbortController 树

```
toolUseContext.abortController（父，来自 query 参数）
  └── siblingAbortController（createChildAbortController，StreamingToolExecutor 创建）
        └── toolAbortController（每个工具独立，executeTool 中创建）

中止原因：
  'interrupt'      → 用户发送新消息，interruptBehavior='cancel' 的工具才取消
  'sibling_error'  → BashTool 出错，自动取消其他兄弟工具（不冒泡到父）
  其他原因         → 从 toolAbortController 冒泡到 toolUseContext.abortController
```

### 结果顺序保证

`getCompletedResults()` 按 `this.tools` 数组顺序遍历：
- 已完成 → 标记 `yielded`，emit results
- 正在执行且非 `isConcurrencySafe` → **break**（串行工具充当屏障，保证顺序）

---

## 消息历史的数据结构

### 每个 turn 结束时追加到 `state.messages`

```
messages = [
  ...messagesForQuery,    // 经过压缩/截断处理后的历史
  ...assistantMessages,   // 本轮 API 返回的 AssistantMessage[]
  ...toolResults,         // UserMessage[]（含 tool_result blocks）和 AttachmentMessage[]
]
```

### tool_result 的 UserMessage 结构

```typescript
{
  type: 'user',
  content: [
    {
      type: 'tool_result',
      tool_use_id: toolUse.id,          // 必须与 assistant 中的 tool_use block id 对应
      content: string | ContentBlock[],
      is_error?: boolean,               // 工具出错时为 true
    },
    // 可选：acceptFeedback text block、image blocks
  ],
  sourceToolAssistantUUID: string,      // 指向产生该 tool_use 的 AssistantMessage.uuid
}
```

### Thinking blocks 的保留规则

代码注释（`query.ts:151-163`）明确了三条规则：
1. 含 thinking/redacted_thinking 块的消息，所在 request 的 `max_thinking_length` 必须 > 0
2. thinking block 不能是消息的最后一个 block
3. thinking block 必须在整个 assistant trajectory 中完整保留

---

## API 调用参数构建（`paramsFromContext`）

```typescript
{
  model: normalizeModelStringForAPI(options.model),
  messages: addCacheBreakpoints(messagesForAPI, ...),
  system,              // appendSystemContext(systemPrompt, systemContext)
  tools: allTools,     // 经 ToolSearch 过滤后的工具列表
  tool_choice: options.toolChoice,
  betas: betasParams,  // getMergedBetas(model) + session latches
  metadata: getAPIMetadata(),
  max_tokens: maxOutputTokens,
  thinking,            // 见 Thinking 配置
  temperature,         // 仅当 thinking 禁用时传（默认 1）
  output_config,       // 含 effort 值
  speed,               // 'fast' | undefined
}
```

### isAgenticQuery 判断（影响 betas 和缓存策略）

```typescript
querySource.startsWith('repl_main_thread') ||
querySource.startsWith('agent:') ||
querySource === 'sdk' ||
querySource === 'hook_agent' ||
querySource === 'verification_agent'
```

### Thinking 参数构建逻辑

```
hasThinking = (thinkingConfig.type !== 'disabled') AND NOT CLAUDE_CODE_DISABLE_THINKING

if hasThinking AND modelSupportsThinking(model):
  if NOT DISABLE_ADAPTIVE_THINKING AND modelSupportsAdaptiveThinking(model):
    thinking = { type: 'adaptive' }           // opus-4-6 / sonnet-4-6
  else:
    thinkingBudget = getMaxThinkingTokensForModel(model)  // upperLimit - 1
    thinking = { type: 'enabled', budget_tokens: min(maxOutputTokens-1, budget) }

模型支持 adaptive thinking：
  claude-opus-4-6 / claude-sonnet-4-6，以及 1P/Foundry 未知新模型（默认 true）

Ultrathink：
  用户输入含 \bultrathink\b → 高强度思维模式（GrowthBook tengu_turtle_carbon）
```

---

## 所有退出条件（Terminal / Continue）

### Terminal 退出（任务彻底结束）

| reason | 触发条件 |
|---|---|
| `completed` | 模型完成任务，无工具调用，Stop Hooks 通过 |
| `blocking_limit` | token 超过硬性上限且 autocompact 关闭 |
| `model_error` | API 异常且无法恢复（非 FallbackTriggeredError） |
| `image_error` | 图片过大 / 媒体 reactiveCompact 无法恢复 |
| `prompt_too_long` | 413 错误，collapse + reactiveCompact 均无效 |
| `aborted_streaming` | streaming 期间用户中断（非 'interrupt' 原因）|
| `aborted_tools` | 工具执行期间用户中断 |
| `hook_stopped` | 某工具的 `hook_stopped_continuation` attachment |
| `stop_hook_prevented` | stop hook 的 `preventContinuation` 为 true |
| `max_turns` | `nextTurnCount > maxTurns` |

### Continue 迭代（下一轮继续）

| transition.reason | 触发条件 |
|---|---|
| `next_turn` | 正常工具调用完成，继续下一轮 |
| `max_output_tokens_recovery` | max_tokens 错误，注入"请继续"，最多 3 次 |
| `max_output_tokens_escalate` | 首次 max_tokens，升级 maxOutputTokensOverride 到 64k |
| `reactive_compact_retry` | 413 / media 错误触发 reactiveCompact |
| `collapse_drain_retry` | 413 错误触发 context collapse drain |
| `stop_hook_blocking` | stop hook 产生 blockingErrors |
| `token_budget_continuation` | token budget 模式注入 nudge 消息 |

---

## 错误处理的各个层级

### 层级 1：工具输入验证

- Zod schema 解析失败 → `InputValidationError` tool_result（`is_error: true`）
- `tool.validateInput()` 返回 false → 自定义错误 tool_result

### 层级 2：权限拒绝

- `permissionDecision.behavior !== 'allow'` → `is_error: true` tool_result
- 若 `decisionReason.type === 'classifier'`：运行 PermissionDenied hooks，hook 可请求重试

### 层级 3：工具执行错误

- `tool.call()` 抛出 `AbortError`：`is_error: true`，`isInterrupt: true`
- `McpAuthError`：更新 appState 为 `needs-auth` 状态
- `classifyToolError` 分类：`TelemetrySafeError` message / errno code / stable `.name` / `'Error'`

### 层级 4：兄弟工具级联错误（StreamingToolExecutor）

BashTool 出错时：`hasErrored = true`，`siblingAbortController.abort('sibling_error')`，
后续工具生成合成 `tool_use_error`：`Cancelled: parallel tool call ${desc} errored`

### 层级 5：API/网络错误（query.ts）

- `FallbackTriggeredError` → 切换到备用模型，`attemptWithFallback = true`，yield 系统警告消息，`continue`
- fallback 场景（ant 用户）：`stripSignatureBlocks(messagesForQuery)` 清除 thinking blocks 签名

### 层级 6：特殊 API 错误的 withheld 恢复流程

```
413 prompt_too_long：先 collapse drain → 再 reactiveCompact → 失败则 return 'prompt_too_long'
max_output_tokens：先升级到 ESCALATED_MAX_TOKENS（一次性）→ 最多 3 次 recovery message
media size error：仅走 reactiveCompact 路径
```

---

## 什么时候会"自我恢复"？

| 问题 | 自动恢复方式 |
|------|------------|
| 模型输出太多（max_tokens） | 先升级上限到 64k，不行再注入"请继续"（最多 3 次） |
| 消息太长（413 错误） | 先 collapse drain，再 reactiveCompact 压缩 |
| 模型不可用（529 过载） | FallbackTriggeredError → 切换备用模型 |
| Stop Hook 检测到问题 | 把 hook 的反馈注入对话，模型修正后再交付 |

---

## 关键文件

| 文件 | 你想了解什么就看这里 |
|------|-------------------|
| `src/query.ts` | queryLoop() 完整实现（1730行），State 类型，所有决策逻辑 |
| `src/QueryEngine.ts` | QueryEngine 类，submitMessage 入口 |
| `src/services/api/claude.ts` | API 参数构建（paramsFromContext）、Thinking 配置、SSE 处理 |
| `src/services/tools/StreamingToolExecutor.ts` | 并发调度器（531行）、AbortController 树、结果顺序 |
| `src/services/tools/toolExecution.ts` | 单个工具的完整执行链（权限→执行→钩子，1746行） |
| `src/utils/thinking.ts` | ThinkingConfig 类型、modelSupportsAdaptiveThinking() |
| `src/query/transitions.ts` | Continue/Terminal 类型定义 |
