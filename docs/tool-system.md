# 工具系统（Tool System）

工具是 Claude Code 的"手脚"——模型通过调用工具来读写文件、执行命令、搜索内容、访问网络。这篇文档讲清楚工具系统的设计和各核心工具的工作方式。

---

## 工具是什么？

工具是一个标准化的接口。模型决定"我要调用这个工具，参数是这些"，系统负责执行，把结果返回给模型。

```
模型返回：
  {
    type: "tool_use",
    name: "Bash",
    input: { command: "ls -la src/" }
  }
        │
        ▼
工具系统：
  1. 验证参数（Zod schema）
  2. 检查权限（是否需要用户确认？）
  3. 执行工具
  4. 返回结果给模型
```

---

## 工具的完整接口（`src/Tool.ts`）

每个工具都实现同一套接口，完整字段分类如下：

**必须实现：**
```typescript
name              工具名（模型调用时用）
inputSchema       参数格式定义（Zod schema）
call()            执行逻辑（返回 { data: Out }）
```

**标识与元数据：**
```typescript
description()     工具描述（发给模型，影响模型何时调用）
prompt()          工具的系统提示词（注入到 getSystemPrompt）
searchHint        全文搜索提示（工具发现时使用）
isMcp             是否为 MCP 工具
alwaysLoad        始终加载（不受 ToolSearch 过滤）
mcpInfo           MCP 服务器信息（serverName + toolName）
```

**行为控制：**
```typescript
isReadOnly()           只读工具（影响并发调度，默认 false）
isConcurrencySafe()    可与其他工具并发（只读工具返回 true）
isDestructive()        破坏性操作（影响 UI 警告）
isOpenWorld()          可能访问外部网络/系统
strict                 Zod strict 模式（额外未知字段校验）
interruptBehavior()    用户中断时的行为（'cancel' | 'wait'）
```

**验证：**
```typescript
validateInput()    Zod 之后的语义检查（如"文件未读就不能编辑"）
checkPermissions() 权限决策（返回 allow/deny/ask/passthrough）
requiresUserInteraction() 强制弹出 UI（绕过 bypass 模式）
```

**结果处理：**
```typescript
maxResultSizeChars    结果截断阈值（与 DEFAULT_MAX_RESULT_SIZE_CHARS=50_000 取 min）
mapToolResultToToolResultBlockParam()  结果转为 API 格式
extractSearchText()   从结果中提取可搜索文本
```

**UI 渲染（8 个方法）：**
```typescript
renderToolUseMessage()          工具被调用时（streaming 实时渲染）
renderToolUseProgressMessage()  执行中的进度 UI
renderToolUseQueuedMessage()    排队等待时的 UI
renderToolResultMessage()       完成后的结果 UI
getActivityDescription()        spinner 文字（如 "Reading src/foo.ts"）
userFacingName()                左侧工具标签显示名（返回 '' 则隐藏）
description()                   工具描述文本
getToolGroup()                  UI 分组
```

---

## 工具的完整执行流程（`src/services/tools/toolExecution.ts`）

```
模型调用工具
      │
      ▼
① Zod schema 验证输入参数
      │ 失败 → 返回 InputValidationError 给模型（is_error: true，不终止循环）
      ▼
② tool.validateInput()（语义检查，如"文件未读就不能编辑"）
      │ 失败 → 返回 errorCode + 说明给模型
      ▼
③ 执行 PreToolUse Hooks（runPreToolUseHooks）
      │ hook 可以：修改 input、阻止执行（block_decision）、追加 context
      │ resolveHookPermissionDecision() 汇总 hook 结论
      ▼
④ 权限检查 hasPermissionsToUseTool()
      │ deny  → 返回"用户拒绝"给模型（is_error: true）
      │ ask   → 弹出权限对话框（等待用户 allow/deny）
      │        ask + 分类器 → classifyYoloAction()（auto 模式）
      │ allow → 继续
      ▼
⑤ tool.call()（实际执行）
      │ AbortError → is_error + isInterrupt: true
      │ 其他异常 → classifyToolError() 分类错误类型
      ▼
⑥ maybePersistLargeToolResult()
      │ 结果 > min(tool.maxResultSizeChars, 50_000) → 持久化到磁盘
      │ 返回 <persisted-output> 格式给模型
      ▼
⑦ 执行 PostToolUse Hooks（runPostToolUseHooks）
      │ hook 可以：修改输出、注入追加消息
      │ PostToolUseFailure hook 在 call() 抛出时触发
      ▼
⑧ 结果返回给模型（作为下一轮的 user 消息）
```

**关键设计**：工具执行失败不会终止 Agent Loop，错误作为工具结果返回给模型，让模型自己决定怎么处理。

---

## 并发执行策略（`StreamingToolExecutor`）

`isConcurrencySafe(parsedInput)` 决定是否可以并发：

```
可以执行 = （没有任何工具在 executing）
          OR（新工具是 isConcurrencySafe 且所有 executing 工具也是 isConcurrencySafe）

只读工具（Read/Grep/Glob/WebFetch） → isConcurrencySafe = true
写操作工具（Bash/Edit/Write）      → isConcurrencySafe = false（串行）

示例：模型一次调用了 5 个工具
  Read("a.ts")  ← 只读 ┐
  Read("b.ts")  ← 只读 ├─ 三个并发执行
  Grep("foo")   ← 只读 ┘
  Edit("a.ts")  ← 写操作 → 等上面三个完成后执行
  Bash("test")  ← 写操作 → 等 Edit 完成后执行
```

`getCompletedResults()` 按工具入队顺序遍历：遇到未完成的非 `isConcurrencySafe` 工具时 **break**（串行工具充当屏障，保证结果顺序）。

**BashTool 出错时的级联**：`hasErrored = true`，`siblingAbortController.abort('sibling_error')`，后续工具生成合成 `tool_use_error`：`Cancelled: parallel tool call ${desc} errored`。

---

## 核心工具详解

### Bash — 执行 Shell 命令

**命令分类**（影响 UI 折叠显示）

```typescript
// 常量定义（BashTool.tsx lines 59-72）
BASH_SEARCH_COMMANDS = new Set(['find', 'grep', 'rg', 'ag', 'ack', 'locate', 'which', 'whereis'])
BASH_READ_COMMANDS   = new Set(['cat', 'head', 'tail', 'less', 'more', 'wc', 'stat', 'file', 'strings', 'jq', 'awk', 'cut', 'sort', 'uniq', 'tr'])
BASH_LIST_COMMANDS   = new Set(['ls', 'tree', 'du'])
BASH_SILENT_COMMANDS = ['mv', 'cp', 'rm', 'mkdir', 'touch', 'ln', ...]  // 静默命令
```

**大输出处理**

```
输出文件 > maxResultSizeChars（30,000 字符）
  │
  ├─ 复制到 tool-results 目录（link 优先，fallback copyFile）
  │   如 > 64 MB → fsTruncate 截断到 64 MB
  └─ 返回给模型：
     <persisted-output>
     Output too large (X bytes). Full output saved to: /path/to/file
     Preview (first 2KB):
     [内容前 2000 字节...]
     </persisted-output>

PREVIEW_SIZE_BYTES = 2000（toolResultStorage.ts line 109）
MAX_PERSISTED_SIZE = 64 * 1024 * 1024（BashTool.tsx line 732）
```

**进度显示与自动后台化**

```typescript
// 进度阈值
PROGRESS_THRESHOLD_MS = 2000          // 2s 后开始显示实时进度
// 超时后台阈值（KAIROS 助手模式）
ASSISTANT_BLOCKING_BUDGET_MS = 15_000 // 15s 后自动后台化

// KAIROS 自动后台化逻辑（BashTool.tsx line 976-983）
if (feature('KAIROS') && getKairosActive() && isMainThread && !isBackgroundTasksDisabled && run_in_background !== true) {
  setTimeout(() => {
    if (shellCommand.status === 'running' && backgroundShellId === undefined) {
      assistantAutoBackgrounded = true
      startBackgrounding('tengu_bash_command_assistant_auto_backgrounded')
    }
  }, ASSISTANT_BLOCKING_BUDGET_MS).unref()  // .unref() 防止 timer 阻止进程退出
}
```

**sed 命令特殊处理**

当模型发出 `sed -i 's/old/new/' file` 这样的命令时：
1. `parseSedCommandForSimulation()` 解析 sed BRE 语法 → JS 正则（用 NULL_BYTE 占位符保护转义字符）
2. 在权限对话框里**展示实际文件变更预览**（不是原始命令字符串）
3. 用户确认后，`input._simulatedSedEdit` 为 true，直接调用 `applySedEdit()` 写入（跳过 shell），确保"你看到的就是写进去的"

**沙箱阻断**：开启沙箱后，Bash 在 bubblewrap 受限环境中执行；部分命令可配置 `excludedCommands` 豁免。

---

### FileRead — 读取文件

**支持文件类型**

```
.py / .ts / .go 等  → 带行号的文本内容
.png / .jpg 等      → base64 图片（发给模型视觉能力）
.pdf               → 有 pages 参数 → 提取指定页转 JPEG；无 → 原生 PDF
.ipynb             → Jupyter notebook（cells 数组格式）
```

**智能去重（6 条件）**

当所有条件同时满足时，返回 `file_unchanged` 存根（不重传内容）：

```
1. GrowthBook tengu_read_dedup_killswitch 未开启
2. readFileState 中存在此文件的记录（existingState）
3. !existingState.isPartialView（之前是完整读取）
4. existingState.offset !== undefined（是 Read 写入的，非 Edit/Write）
5. 参数一致：offset 和 limit 相同
6. 文件 mtime 未变化（getFileModificationTimeAsync() === existingState.timestamp）
```

**两级 Token 限制**

```
maxSizeBytes: MAX_OUTPUT_SIZE = 256 KB        → 读取原始字节上限
maxTokens: DEFAULT_MAX_OUTPUT_TOKENS = 25000  → Token 估算上限
  （可被环境变量 CLAUDE_CODE_FILE_READ_MAX_OUTPUT_TOKENS 或 GrowthBook tengu_amber_wren 覆盖）

maxResultSizeChars: Infinity  → 不触发 maybePersistLargeToolResult，自身有 token 机制
```

**安全检查**：阻止读取 `/dev/zero`、`/dev/random` 等无限输出设备；文本文件内容后追加防注入提醒。

---

### FileEdit — 精确编辑文件

**10 种 errorCode 分类**

| errorCode | 触发条件 |
|-----------|---------|
| `0` | TeamMem 密钥保护拒绝编辑 |
| `1` | `old_string === new_string`（无变化） |
| `2` | 文件路径匹配 `alwaysDeny` 规则 |
| `3` | `old_string === ''` 但文件已存在（创建冲突） |
| `4` | 文件不存在且 `old_string !== ''` |
| `5` | 文件是 Jupyter Notebook（应用 NotebookEdit） |
| `6` | 文件未读过（`!readTimestamp || readTimestamp.isPartialView`） |
| `7` | 文件自上次读取后被修改（mtime 检查失败） |
| `8` | `old_string` 在文件中找不到 |
| `9` | 找到多处匹配但 `replace_all` 为 `false` |
| `10` | 文件超过 `MAX_EDIT_FILE_SIZE`（1 GiB） |

**mtime 双重检查（防 Windows 假阳性）**

```typescript
const lastWriteTime = getFileModificationTime(fullFilePath)
if (lastWriteTime > readTimestamp.timestamp) {
  // Windows 文件系统时间戳可能在内容不变时也改变
  // 完整读取时对比内容作为 fallback
  const isFullRead = readTimestamp.offset === undefined && readTimestamp.limit === undefined
  if (isFullRead && fileContent === readTimestamp.content) {
    // 内容相同，安全继续
  } else {
    return { result: false, errorCode: 7 }  // 真正的外部修改
  }
}
```

**原子写入序列**

1. 检测文件编码（`detectFileEncoding`）
2. 检测行尾符（`detectLineEndings`，保持原始 CRLF/LF）
3. 构建新内容字符串
4. `writeTextContent()` 同步写入磁盘（不 await，防并发写入竞态）
5. 通知 LSP（TypeScript 语言服务更新诊断）
6. 通知 VSCode（更新 diff 视图）
7. 更新 `readFileState.set()`（记录新 mtime + 新内容）

`maxResultSizeChars: 100_000`（编辑确认消息本身很短，但防御性设置）

---

### Grep — 内容搜索

封装 ripgrep，三种输出模式：

```
files_with_matches（默认）→ 路径列表，按 mtime 排序，最多 250 条
content              → 匹配行内容（支持 -A/-B/-C/-n/-i/-U 参数）
count                → 每文件匹配数统计
```

自动排除 `.git`、`.svn` 等版本控制目录；每行最长 500 字符（避免 base64/minified 内容污染）。

---

### WebFetch — 抓取网页内容

**关键常量**

```typescript
MAX_URL_LENGTH   = 2000           // URL 长度上限（WebFetchTool utils.ts line 106）
CACHE_TTL_MS     = 15 * 60 * 1000 // 15 分钟 URL 缓存
MAX_CACHE_SIZE_BYTES = 50 * 1024 * 1024  // 50 MB LRU 缓存总量
MAX_MARKDOWN_LENGTH = 100_000     // Haiku 摘要的触发阈值
maxResultSizeChars = 100_000      // 结果截断阈值
```

**Haiku 摘要触发逻辑**

```typescript
// 跳过 Haiku（直接返回原始内容）当三个条件全部满足：
if (
  isPreapproved &&                          // 域名在预授权白名单
  contentType.includes('text/markdown') &&  // Content-Type 是 text/markdown
  content.length < MAX_MARKDOWN_LENGTH      // 内容 < 100,000 字符
) {
  result = content  // 直接返回
}
// 否则调用 Haiku 模型对内容回答 prompt（摘要/提取相关部分）
```

**跨域重定向处理**：不自动跟随跨域重定向（防止授权了域名 A 的请求跳到域名 B），返回特殊响应让模型重新调用新 URL。

---

### MCPTool — MCP 服务器代理

**动态生成机制**（`src/services/mcp/client.ts`）

MCP 工具通过对象展开（spread）模式从 `MCPTool` 基础模板生成：

```typescript
return toolsToProcess.map((tool): Tool => {
  const fullyQualifiedName = buildMcpToolName(client.name, tool.name)
  return {
    ...MCPTool,                           // 展开基础模板（默认行为）
    name: skipPrefix ? tool.name : fullyQualifiedName,  // mcp__server__tool 格式
    mcpInfo: { serverName: client.name, toolName: tool.name },
    isMcp: true,
    alwaysLoad: tool._meta?.['anthropic/alwaysLoad'] === true,
    async description() { return tool.description ?? '' },
    // MCP annotations → Tool 接口字段
    isConcurrencySafe() { return tool.annotations?.readOnlyHint ?? false },
    isReadOnly()        { return tool.annotations?.readOnlyHint ?? false },
    isDestructive()     { return tool.annotations?.destructiveHint ?? false },
    isOpenWorld()       { return tool.annotations?.openWorldHint ?? false },
  }
})
```

`checkPermissions()` 返回 `passthrough`，由通用权限系统处理（用户可配置 `mcp__server__tool` 格式规则）。

`fetchToolsForClient()` 使用 LRU memoize 缓存，避免重复拉取 `tools/list`。

---

### AgentTool — 启动子 Agent

**路由决策树**

```
AgentTool.call()
  │
  ├─ teamName && name → spawnTeammate()（team 模式，返回 status: 'teammate_spawned'）
  │
  ├─ isForkSubagentEnabled() && 无 subagent_type
  │    → FORK_AGENT 定义 + 从父上下文构建 fork messages（缓存共享路径）
  │
  ├─ effectiveIsolation === 'worktree' → 创建 git worktree → runAgent()
  │
  ├─ effectiveIsolation === 'remote'（ant-only）→ teleportToRemote() + 本地轮询
  │
  ├─ shouldRunAsync = true → registerAsyncAgent()（后台，立刻返回 task_id）
  │
  └─ 同步模式 → runAgent()（前台，阻塞直到完成，返回 status: 'completed'）
```

**`shouldRunAsync` 的 6 个触发条件**：

```typescript
const shouldRunAsync =
  assistantForceAsync ||         // KAIROS 模式强制异步（kairosActive=true 时）
  run_in_background === true ||  // 模型显式要求后台
  isolation === 'worktree' ||    // worktree 隔离（自动后台）
  (isAnonymousAgent && isAsync) || // 匿名 Agent 且父级异步
  subagent_type?.includes('background') || // 特定类型强制后台
  forceAsync                     // 参数强制
```

```typescript
// AgentTool.tsx
const assistantForceAsync = feature('KAIROS') ? appState.kairosEnabled : false
```

**权限继承规则**：子 Agent 只有显式声明的 `allowedTools` 白名单；父 Agent 运行时已批准的 session 级规则**不会传递**给子 Agent（防止权限升级攻击）。

---

## 工具结果持久化（`maybePersistLargeToolResult`）

关键常量（`src/constants/toolLimits.ts`）：

| 常量 | 值 | 含义 |
|------|-----|------|
| `DEFAULT_MAX_RESULT_SIZE_CHARS` | `50_000` | 全局结果上限（工具 `maxResultSizeChars` 取 min）|
| `MAX_TOOL_RESULTS_PER_MESSAGE_CHARS` | `200_000` | 每条消息所有工具结果的总字符预算 |
| `MAX_TOOL_RESULT_TOKENS` | `100_000` | Token 限制 |
| `PREVIEW_SIZE_BYTES` | `2000` | 大结果预览字节数 |

**实际触发阈值**：`Math.min(tool.maxResultSizeChars, DEFAULT_MAX_RESULT_SIZE_CHARS)`

各工具的 `maxResultSizeChars`：

| 工具 | maxResultSizeChars |
|------|-------------------|
| BashTool | `30_000` |
| FileEditTool / WebFetchTool | `100_000`（但受 50_000 全局 cap） |
| GrepTool | `20_000` |
| FileReadTool | `Infinity`（不触发持久化，有自身 token 机制）|

**持久化输出格式**（`buildLargeToolResultMessage`）：

```
<persisted-output>
Output too large (X bytes). Full output saved to: /path/to/tool-results/file
Preview (first 2KB):
[内容前 2000 字节...]
...
</persisted-output>
```

---

## 工具注册机制

```
启动时 getAllBaseTools() 收集所有内置工具
        │
        ├─ 无条件加载：Bash / Read / Edit / Write / Glob / Grep / WebFetch 等
        ├─ 条件加载（feature flag）：MonitorTool / WebBrowserTool / SleepTool 等
        └─ 条件加载（用户类型）：ConfigTool / TungstenTool（仅 ant 内部）

然后 assembleToolPool() 合并 MCP 工具：
        ├─ 内置工具排前，同名冲突时内置工具获胜
        └─ 按名字排序（保证顺序稳定，不破坏 Prompt Cache）

最后 filterToolsByDenyRules() 按配置的 deny 规则过滤

ToolSearch（feature 可选）：基于当前上下文动态过滤工具列表
        → alwaysLoad=true 的工具始终包含
        → 其余工具按相关性过滤（减少 API 工具列表 token 开销）
```

---

## 关键文件

| 文件 | 里面有什么 |
|------|-----------|
| `src/Tool.ts` | 工具接口完整定义（30+ 字段）、ToolUseContext、buildTool 工厂 |
| `src/tools.ts` | 所有工具注册与组装（getAllBaseTools / assembleToolPool）|
| `src/constants/toolLimits.ts` | 所有工具结果限制常量 |
| `src/tools/BashTool/BashTool.tsx` | Bash 工具（命令分类、KAIROS 自动后台、sed 模拟、大输出）|
| `src/tools/BashTool/runShellCommand.ts` | Shell 执行引擎（进度、后台化）|
| `src/tools/FileEditTool/FileEditTool.ts` | 文件编辑（10 errorCode、mtime 双检、原子写入）|
| `src/tools/FileReadTool/FileReadTool.ts` | 文件读取（6 条件去重、多格式、token 限制）|
| `src/tools/FileReadTool/limits.ts` | FileRead token 限制常量 |
| `src/tools/WebFetchTool/WebFetchTool.ts` | 网页抓取（Haiku 摘要、重定向检测）|
| `src/tools/WebFetchTool/utils.ts` | WebFetch 常量（MAX_URL_LENGTH、CACHE_TTL_MS）|
| `src/services/mcp/client.ts` | MCPTool 动态生成（spread 模式，fetchToolsForClient LRU）|
| `src/tools/AgentTool/AgentTool.tsx` | 子 Agent 路由（shouldRunAsync 6 条件、assistantForceAsync）|
| `src/tools/AgentTool/runAgent.ts` | 子 Agent 执行环境初始化（20 步骤）|
| `src/services/tools/StreamingToolExecutor.ts` | 并发调度核心（TrackedTool、isConcurrencySafe）|
| `src/services/tools/toolExecution.ts` | 工具执行链（验证→权限→hook→执行→hook，1746 行）|
| `src/services/tools/toolHooks.ts` | PreToolUse/PostToolUse hook 执行 |
| `src/utils/toolResultStorage.ts` | 大结果持久化（buildLargeToolResultMessage）|
