# 上下文管理与 Prompt 系统

"上下文管理"回答的是一个核心问题：**每次调用模型时，到底把什么内容发过去？**

这包括系统提示词的构建、对话历史的取舍、Token 预算的分配，以及如何在不破坏缓存的情况下动态更新内容。

---

## 一张图看懂系统 Prompt 的构成

发给模型的内容由三部分组成：

```
┌─────────────────────────────────────────────────────────────┐
│                      API 请求内容                            │
│                                                             │
│  ┌─────────────────────────────────────┐                    │
│  │          System Prompt              │                    │
│  │                                     │                    │
│  │  ① 静态区（全局缓存）                  │                    │
│  │    - 角色定义 / 行为规范               │                    │
│  │    - 工具使用指南                     │                    │
│  │    - 输出风格要求                     │                    │
│  │  ─────────────── 分割线 ──────────   │                    │
│  │  ② 动态区（每轮更新）                 │                    │
│  │    - CLAUDE.md 记忆内容              │                    │
│  │    - 当前环境信息（cwd/git/OS）       │                    │
│  │    - MCP 服务器指令                  │                    │
│  │    - 语言偏好 / 输出风格设置           │                    │
│  └─────────────────────────────────────┘                    │
│                                                             │
│  ┌─────────────────────────────────────┐                    │
│  │     Messages（对话历史）              │                    │
│  │    user → assistant → user → ...    │                    │
│  └─────────────────────────────────────┘                    │
│                                                             │
│  ┌─────────────────────────────────────┐                    │
│  │     Tools（工具定义列表）              │                    │
│  └─────────────────────────────────────┘                    │
└─────────────────────────────────────────────────────────────┘
```

**为什么分静态区和动态区？**
因为 Anthropic 的 Prompt Cache 是基于内容前缀缓存的。静态区内容不变，可以全局缓存；动态区内容会变（比如 MCP 工具列表），放到分割线后面，避免动态内容影响前面稳定内容的缓存命中。

分割线标记字符串：`'__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__'`（`src/constants/prompts.ts` 第 114 行）

---

## 系统提示词的完整构建流程（`getSystemPrompt`）

`src/constants/prompts.ts` 的 `getSystemPrompt(tools, model, ...)` 是入口，有三条路径：

**简单路径**（`CLAUDE_CODE_SIMPLE=true`）：
```
"You are Claude Code, Anthropic's official CLI for Claude.\n\nCWD: ${cwd}\nDate: ${date}"
```

**标准路径**（主路径）：按顺序拼装为数组：

```
静态 sections（可全局缓存）：
  getSimpleIntroSection()          → 角色定义 + 安全提示
  getSimpleSystemSection()         → # System 规则
  getSimpleDoingTasksSection()     → # Doing tasks
  getActionsSection()              → # Executing actions with care
  getUsingYourToolsSection()       → # Using your tools（含 enabledTools）
  getSimpleToneAndStyleSection()   → # Tone and style
  getOutputEfficiencySection()     → # Output efficiency

  '__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__'  ← 边界标记

动态 sections（systemPromptSection 缓存框架管理）：
  'memory'       → loadMemoryPrompt()
  'env_info_simple' → computeSimpleEnvInfo()（工作目录、git、平台、模型）
  'language'     → 语言偏好
  'output_style' → 输出风格配置
  'mcp_instructions' → MCP 指令（DANGEROUS_uncached，MCP 服务器随时变化）
  'scratchpad'   → 临时文件目录
  'frc'          → function result clearing（工具结果会被清除的提示）
  'token_budget' → token 预算追踪
  'brief'        → KAIROS Brief 工具指令（如启用）
```

**KAIROS/PROACTIVE 路径**（助手模式）：
直接拼接自主 agent 介绍、proactive section、memory 等，不走 section 缓存框架。

**子 Agent 路径**（`enhanceSystemPromptWithEnvDetails`）：
在父级 system prompt 基础上追加注意事项 + 技能发现指导 + 完整环境信息（比 Simple 版本更详细）。

---

## CLAUDE.md 的加载顺序与合并逻辑

`getMemoryFiles(forceIncludeExternal?)` 是 memoized 异步函数，按以下顺序加载（后加载的优先级更高）：

```
1. Managed   /etc/claude-code/CLAUDE.md           企业管理员（始终加载，不可禁用）
             /etc/claude-code/.claude/rules/*.md

2. User      ~/.claude/CLAUDE.md                  isSettingSourceEnabled('userSettings')
             ~/.claude/rules/*.md
             （允许 @include 外部文件）

3. Project   从根目录向 cwd 方向逐层加载：         isSettingSourceEnabled('projectSettings')
             {dir}/CLAUDE.md
             {dir}/.claude/CLAUDE.md
             {dir}/.claude/rules/*.md
             → 越靠近 cwd 的文件越后加载（优先级更高）

4. Local     {dir}/CLAUDE.local.md                gitignored 本地覆盖

5. AutoMem   getAutoMemEntrypoint()               isAutoMemoryEnabled()
             → 截断：200行/25KB 上限

6. TeamMem   team memory MEMORY.md               feature('TEAMMEM') && 启用
```

**`@include` 指令**：
- 语法：`@path`、`@./relative`、`@~/home`、`@/absolute`
- 用 marked Lexer 解析，跳过代码块中的 `@`
- 最大嵌套深度：5 层（`MAX_INCLUDE_DEPTH`）
- 循环引用防护：`processedPaths Set`
- 不允许外部文件（除非 `hasClaudeMdExternalIncludesApproved`）

**块级 HTML 注释**（`<!-- ... -->`）在加载时被剥除，内联注释和代码块内的注释保留。

**`getAutoMemPath` 解析优先级**：
```
1. CLAUDE_COWORK_MEMORY_PATH_OVERRIDE 环境变量
2. settings.json 中 autoMemoryDirectory（policy > flag > local > user）
3. <~/.claude>/projects/<sanitized-git-canonical-root>/memory/
```

---

## 上下文窗口与 Token 预算

### 模型能"记住"多少？

```
默认上下文窗口：200,000 tokens（约 15 万汉字）
超大模型/实验：1,000,000 tokens

有效窗口 = 总窗口 - min(maxOutputTokensForModel, 20_000)（保留给压缩摘要输出）
```

### 自动压缩阈值（`shouldAutoCompact`）

```typescript
autoCompactThreshold = effectiveContextWindow - 13_000  // AUTOCOMPACT_BUFFER_TOKENS

可通过 CLAUDE_AUTOCOMPACT_PCT_OVERRIDE 环境变量设置百分比（如 '90' = 90%）
```

各阈值常量：

| 常量 | 值 | 用途 |
|---|---|---|
| `AUTOCOMPACT_BUFFER_TOKENS` | 13,000 | autocompact 触发余量 |
| `WARNING_THRESHOLD_BUFFER_TOKENS` | 20,000 | 警告显示余量 |
| `MANUAL_COMPACT_BUFFER_TOKENS` | 3,000 | 手动 compact 阻断余量 |
| `MAX_OUTPUT_TOKENS_FOR_SUMMARY` | 20,000 | compact 输出预留 |
| `MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES` | 3 | 连续失败熔断阈值 |

**不触发 autocompact 的情况**：
- `querySource === 'session_memory'` 或 `'compact'`（防递归）
- `feature('REACTIVE_COMPACT')` 且 GrowthBook `tengu_cobalt_raccoon=true`（reactive-only 模式，用 API 错误驱动）
- `isContextCollapseEnabled()` 为 true

### Token 计数方法（三个层级）

| 方法 | 精度 | 场景 |
|------|------|------|
| `getTokenCountFromUsage(usage)` | 最准确 | 从上次 API 响应的 `message.usage` 读取 |
| `tokenCountWithEstimation(messages)` | 混合 | **autocompact 触发判断的规范函数**：上次真实值 + 此后增量估算 |
| `roughTokenCountEstimation(content)` | 粗略 | `content.length / 4`（JSON 文件 2 bytes/token）|

**粗略估算的各类型规则**（`roughTokenCountEstimationForBlock`）：
- `text`：`length / 4`
- `tool_use`：`(name + JSON.stringify(input)).length / 4`
- `image`/`document`：固定 2,000 tokens（保守）
- `thinking`：`thinking.length / 4`

---

## 对话压缩：太长了怎么办？

### 压缩触发 → 执行的完整流程

```
检测到 token 超过 autoCompactThreshold
       │
       ▼
先试：会话记忆剪枝（无需 AI，直接删除旧记录）
       │ 不够用
       ▼
正式压缩（compactConversation）：
  1. stripImagesFromMessages()     — 去除图片，防止压缩请求本身太长
  2. uniqBy(toolSchemas, t => t.name) — 工具 schema 去重，防 API 400 错误
  3. 执行用户配置的 PreCompact hooks
  4. 构建压缩提示词（BASE_COMPACT_PROMPT / PARTIAL_COMPACT_PROMPT）
  5. 调用 AI（max 20,000 tokens 输出）生成摘要
  6. PTL 截断循环（compress 请求自身也可能 too long）：
     ├─ 能解析 tokenGap → 按轮次分组，删除最旧的组直到覆盖 gap
     └─ 不能解析 → 删除 20% 的组
     最多重试 3 次（MAX_PTL_RETRIES）
  7. formatCompactSummary()：
     ├─ 删除 <analysis>...</analysis> 区块
     └─ 替换 <summary> 标签为 "Summary:\n"
  8. buildPostCompactMessages() 重建消息数组：
     [COMPACT_BOUNDARY, summaryMessages, keepMessages, attachments, hookResults]
  9. 清除 systemPromptSection 缓存（clearSystemPromptSections）
```

### AI 生成的摘要包含 9 个章节

| # | 章节名 | 内容 |
|---|--------|------|
| 1 | Primary Request and Intent | 详细描述用户请求（含隐含需求） |
| 2 | Key Technical Concepts | 技术栈、框架、关键概念 |
| 3 | Files and Code Sections | 检视/修改/创建的文件及代码片段 |
| 4 | Errors and Fixes | 错误列表及修复方法（防止重蹈覆辙）|
| 5 | Problem Solving | 解决的问题和调试思路 |
| 6 | All User Messages | 所有用户消息的完整列举 |
| 7 | Pending Tasks | 待完成任务 |
| 8 | Current Work | 摘要请求前正在做的工作（含文件名和代码）|
| 9 | Optional Next Step | 建议下一步（直接引用最近对话原文）|

**自定义指令**：用户在 CLAUDE.md 中的 `## Compact Instructions` 或 `# Summary instructions` 段落，以及 PreCompact hooks 的输出，会追加到摘要提示词末尾。

### 三种压缩策略的区别

| 策略 | 触发时机 | 消息数组 | 是否调用 AI | 缓存影响 |
|---|---|---|---|---|
| time-based microCompact | 每次 query 前（时间门）| 内容替换（清空旧工具结果）| 否 | 全量失效 |
| cached microCompact（`CACHED_MICROCOMPACT`）| 每次 query 前 | 不变（API 层 cache_edits）| 否 | 无影响 |
| autoCompact | token 超阈值 | 重建（摘要 + 边界）| 是（9章摘要）| 全量失效 |
| reactiveCompact（`REACTIVE_COMPACT`）| API 返回 prompt_too_long | 重建 | 是 | 全量失效 |

---

## Prompt Cache：省钱的关键

### 缓存分层策略（`splitSysPromptPrefix`）

三种情形的 `cache_control` 分配：

```
情形 1（MCP 工具存在，skipGlobalCacheForSystemPrompt=true）：
  Block 1: attribution header   (scope=null，不缓存)
  Block 2: system prompt prefix (scope='org')
  Block 3: rest joined          (scope='org')

情形 2（全局缓存模式 + 找到边界标记，1P only）：
  Block 1: attribution header   (scope=null)
  Block 2: system prompt prefix (scope=null)
  Block 3: 边界前静态内容        (scope='global')  ← 跨 org 共享！
  Block 4: 边界后动态内容        (scope=null)

情形 3（默认，3P 提供商或找不到边界）：
  Block 1: attribution header   (scope=null)
  Block 2: system prompt prefix (scope='org')
  Block 3: rest joined          (scope='org')
```

**`cache_control` 字段结构**：

```typescript
{
  type: 'ephemeral',
  ...(should1hCacheTTL(querySource) && { ttl: '1h' }),  // 符合条件时 1h TTL
  ...(scope === 'global' && { scope }),                  // 全局缓存时添加 scope
}
```

### 消息级别的 cache breakpoint

- 每次请求**只添加一个**消息级别的 cache_control 标记
- 默认打在**最后一条消息**（`markerIndex = messages.length - 1`）
- fire-and-forget fork（`skipCacheWrite=true`）：打在**倒数第二条**（避免 fork 污染主会话缓存）

### 1 小时长效缓存

默认 5 分钟 TTL。满足以下条件升级到 1h：
- Bedrock 用户：`ENABLE_PROMPT_CACHING_1H_BEDROCK=true`
- 1P 用户：是 ant 用户 OR（订阅用户 AND 未超额），且 querySource 在 GrowthBook 白名单中

**为什么要"锁定"缓存条件？**

如果会话中途切换了某个开关（比如开启 fast_mode），系统提示词内容就变了，之前的缓存就废了。为了避免这种"缓存失效"，这些开关（`sessionLatch`）一旦在 session 开始时确定，就不再改变——即使后来关闭了，也继续发送同样的 header 直到本次 session 结束。

---

## 思维链（Extended Thinking）

### ThinkingConfig 类型与默认配置

```typescript
type ThinkingConfig =
  | { type: 'adaptive' }
  | { type: 'enabled'; budgetTokens: number }
  | { type: 'disabled' }

// 默认值（shouldEnableThinkingByDefault）：
// 1. MAX_THINKING_TOKENS 环境变量存在（>0）→ 启用
// 2. settings.alwaysThinkingEnabled === false → 禁用
// 3. 否则默认 true（adaptive）
```

### 各模型的 thinking 支持

| 模型 | thinking 类型 | max budget |
|------|-------------|------------|
| opus-4-6 / sonnet-4-6 | adaptive（无 budget 限制）| — |
| opus-4 / sonnet-4 / haiku-4 | enabled（需设置 budget）| 63,999 |
| claude-3-* | 不支持 | — |

**temperature 规则**：思维模式启用时不传 `temperature`（API 要求必须为 1，而 1 是默认值）。

**Beta Headers**：
- `INTERLEAVED_THINKING_BETA_HEADER`：允许 thinking 与 tool_use 交错
- `REDACT_THINKING_BETA_HEADER`：加密 thinking 块
- 均通过 `getModelBetas()` 自动添加

**fallback 场景**（ant 用户）：`stripSignatureBlocks(messagesForQuery)` 清除 thinking blocks 签名，防止跨模型签名不兼容。

### Ultrathink

`hasUltrathinkKeyword(text)` 检测用户输入中的 `\bultrathink\b`，触发高强度思维模式（`feature('ULTRATHINK')` + GrowthBook `tengu_turtle_carbon` 控制）。

---

## 附件处理

### @ 提及文件时发生了什么

```
用户输入 "@src/utils/foo.ts"
       │
       ├─ 检查是否有 deny 规则
       ├─ 检查文件大小（stat）
       ├─ 是 PDF 且页数很多？→ 只发"这是个 PDF，共 X 页"（不发内容）
       ├─ 文件已在上下文且没改过？→ 告诉模型"已经有了，不重复发送"
       └─ 正常情况 → 读取文件内容，附加到消息中
                      └─ 超出 token 限制？→ 不发（at-mention 模式）
```

### 图片处理

粘贴图片时：
1. 转为 base64 + 自动降采样（太大的图会被压缩）
2. 单图 token 上限约 2,000 tokens（保守估算）
3. 整个请求最多 100 个媒体块，超出时删最旧的

### 相关记忆自动注入

每轮对话时，系统会异步预取与当前任务相关的记忆文件，包在 `<system-reminder>` 标签里注入（不破坏缓存）：
- 单轮最多 5 个文件，总量 20KB
- 整个 session 累计上限 60KB

---

## 关键文件

| 文件 | 里面有什么 |
|------|-----------|
| `src/constants/prompts.ts` | `getSystemPrompt()` 主构建函数，所有 section 文本 |
| `src/constants/systemPromptSections.ts` | 动态 section 注册框架（`systemPromptSection` / `DANGEROUS_uncachedSystemPromptSection`）|
| `src/utils/claudemd.ts` | CLAUDE.md 加载顺序、`@include` 解析、glob 过滤 |
| `src/services/api/claude.ts` | `buildSystemPromptBlocks()`、`getCacheControl()`、thinking 配置 |
| `src/utils/api.ts` | `splitSysPromptPrefix()` cache scope 分配 |
| `src/services/compact/autoCompact.ts` | `shouldAutoCompact()`、token 阈值计算、熔断器 |
| `src/services/compact/compact.ts` | `compactConversation()`、PTL 截断、post-compact 消息重建 |
| `src/services/compact/prompt.ts` | 9 段摘要提示词、`formatCompactSummary()` |
| `src/services/compact/microCompact.ts` | time-based 和 cached 两条微压缩路径 |
| `src/utils/tokens.ts` | `tokenCountWithEstimation()`（规范计数函数）|
| `src/services/tokenEstimation.ts` | `roughTokenCountEstimation()`（粗略估算）|
| `src/utils/context.ts` | `getContextWindowForModel()`、各模型 max tokens |
| `src/utils/thinking.ts` | `ThinkingConfig` 类型、`modelSupportsAdaptiveThinking()` |
