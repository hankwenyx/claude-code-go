# KAIROS — AI 助手模式深度解读

KAIROS 是 Claude Code 内部代号为"助手模式"的高级功能体系，将 Claude Code 从一个"等待你输入才行动"的编码工具，升级为一个**持续运行、主动决策、能与外部世界双向通信**的个人 AI 助手。

> "KAIROS" 源自希腊语，意为"恰当的时机"——呼应其核心能力：AI 在合适的时机主动行动，而不是被动等待。

目前 KAIROS 仅面向 Anthropic 内部用户（`USER_TYPE=ant`），但其子功能正在逐步对外开放。

---

## 普通模式 vs KAIROS 模式

```
普通模式：
  你输入 → Claude 思考 → 输出文字 → 等你下一条输入
  （反应式，被动）

KAIROS 模式：
  Claude 持续运行 → 自主决定做什么 → 通过工具发消息给你
  → 接收 Slack/Telegram 等外部通知 → 继续工作
  （主动式，自驱动）
```

| 维度 | 普通模式 | KAIROS 模式 |
|------|---------|------------|
| 驱动方式 | 用户输入驱动 | tick + 外部通知驱动 |
| 消息输出 | 直接写到终端 | 必须通过 `SendUserMessage` 工具 |
| 界面形态 | 完整对话记录 | 精简 chat 视图（折叠工具调用细节）|
| 长时命令 | 阻塞等待完成 | 15s 后自动后台化（继续协调）|
| 子 Agent | 可同步可异步 | 强制异步（主 Agent 不阻塞）|
| 外部通知 | 只能看终端 | Slack/Telegram/Discord 等推送 |
| 记忆整合 | autoDream 后台自动 | `getKairosActive()` 时 autoDream 跳过，改用 `/dream` skill |
| 计划模式 | 正常可用 | 有 channel 时禁用（无人守键盘）|

---

## KAIROS 的子功能体系

```
KAIROS（主 flag，助手模式核心）
  ├── KAIROS_BRIEF         SendUserMessage 工具（可独立发布给外部用户）
  ├── KAIROS_DREAM         /dream 记忆整合（可独立发布）
  ├── KAIROS_CHANNELS      MCP Channel 通知（Slack/Telegram 等）
  ├── KAIROS_GITHUB_WEBHOOKS  GitHub webhook 消息渲染
  └── KAIROS_PUSH_NOTIFICATION  任务完成推送通知

相关 flag（深度耦合，但独立存在）：
  PROACTIVE               自主循环模式（KAIROS 包含其全部能力）
  AGENT_TRIGGERS          CronCreate/Delete/List 定时任务工具
```

---

## 一、启动与激活流程（`src/main.tsx` lines 1054-1094）

```
main.tsx 初始化
    │
    ① feature('KAIROS') 检查（编译时 flag，构建时死代码消除）
    │
    ② assistantModule.isAssistantMode()
    │     ├─ CLAUDE_CODE_ASSISTANT_MODE=1 环境变量
    │     └─ --assistant CLI flag
    │
    ③ kairosGate.isKairosEnabled()
    │     ├─ GrowthBook 动态门 tengu_kairos
    │     └─ --assistant flag 直接传入（跳过 GrowthBook）
    │
    ④ checkHasTrustDialogAccepted()
    │     └─ 目录必须被用户信任（防止恶意 repo 的 assistant.md 注入系统提示词）
    │
    ⑤ setKairosActive(true)      ← bootstrap/state.ts line 72，全局 Atom
    ⑥ opts.brief = true           自动启用 SendUserMessage
    └─ assistantModule.initializeAssistantTeam()  初始化助手团队
```

`kairosActive` 定义（`src/bootstrap/state.ts` line 72）：

```typescript
// STATE 对象上的普通 boolean 字段（非 Atom）
kairosActive: boolean   // 类型定义（line 72）
kairosActive: false,    // 默认值（STATE 初始化）

// 访问器函数
export function getKairosActive(): boolean { return STATE.kairosActive }
export function setKairosActive(value: boolean): void { STATE.kairosActive = value }
```

---

## 二、KAIROS_BRIEF — 消息通道（`src/tools/BriefTool/BriefTool.ts`）

### 为什么需要这个？

KAIROS 模式下 Claude 可能花几小时在后台处理任务，对话 transcript 里有几百条工具调用。`SendUserMessage`（Brief）工具确保**只有 Claude 主动发送的内容才出现在聊天视图**，其他所有工具调用默认折叠。

### 工具实现细节

```typescript
// BriefTool.ts 关键字段
name: BRIEF_TOOL_NAME              // = 'SendUserMessage'（prompt.ts line 1）
LEGACY_BRIEF_TOOL_NAME = 'Brief'   // 向后兼容别名

// schema 中的 status 字段
status: z.enum(['normal', 'proactive'])
//   'normal'    → 回复用户的消息（对话式）
//   'proactive' → 主动发起的消息（后台任务完成通知等）

userFacingName(): ''               // 返回空字符串 → 隐藏工具 chrome（不显示工具标签）
isConcurrencySafe(): true          // 可与其他工具并发

// mapToolResultToToolResultBlockParam（lines 175-183）
// 返回给模型的内容是确认信息，而非消息本体
return {
  tool_use_id: toolUseID,
  type: 'tool_result',
  content: `Message delivered to user.${suffix}`,  // suffix = "(N attachments included)"
}
```

### isBriefEnabled() 的 6 种激活方式

```typescript
// 触发条件：(getKairosActive() || getUserMsgOptIn()) && isBriefEntitled()
// getUserMsgOptIn()：以下任一即可
方式 1：启动时加 --brief 参数
方式 2：settings.json 设置 defaultView: 'chat'
方式 3：运行 /brief 斜杠命令（需 GrowthBook tengu_kairos_brief 开关）
方式 4：在 /config 设置界面选 defaultView = 'chat'
方式 5：CLAUDE_CODE_BRIEF=1 环境变量（测试用）
方式 6：kairosActive = true 时自动激活（isBriefEntitled() 检查权益）

// KAIROS_BRIEF_REFRESH_MS = 5 * 60 * 1000（5 分钟刷新一次权益状态）
```

激活后系统提示词注入：

> *"SendUserMessage is where your replies go. Text outside it is visible if the user expands the detail view, but most won't — assume unread."*

---

## 三、PROACTIVE — 自主循环引擎（`src/proactive/index.ts`）

### 三状态状态机

```typescript
// 4 个模块级状态变量（let，不导出）
let active = false
let paused = false
let contextBlocked = false
let nextTickAt: number | null = null   // tick 时间戳，供 UI 显示

// 导出的控制函数（共 9 个）
subscribeToProactiveChanges(listener)  // 订阅状态变化
isProactiveActive()      // returns active
isProactivePaused()      // returns paused
activateProactive(_source?)  // active=true, paused=false, nextTickAt=null, emit
deactivateProactive()        // active=false, paused=false, nextTickAt=null, emit
pauseProactive()             // paused=true, emit（Ctrl+C 时调用）
resumeProactive()            // paused=false, emit（用户提交输入时调用）
setContextBlocked(value)     // contextBlocked=value, emit（注：含 void contextBlocked 行抑制 lint 警告）
getNextTickAt()              // returns nextTickAt
// 注：源码中无 getProactiveState() 函数，状态通过各 getter 单独读取
```

### tick 驱动机制

```
KAIROS 自主循环
    │
    ├─ 收到外部通知（Channel/用户输入）→ 立刻处理
    │
    └─ 没有输入 → Claude 调用 Sleep 工具等待
                       │
                       └─ Sleep 期间：
                           ├─ 每秒检查 hasCommandsInQueue()
                           ├─ 收到通知 → 中断睡眠，立刻处理
                           └─ 等待结束 → 下一次 tick（`<tick>` 注入），自主决定
```

### SleepTool（`src/tools/SleepTool/`）

```typescript
// 实现：轮询 hasCommandsInQueue()，每 1 秒一次
async call({ seconds }, context) {
  const endTime = Date.now() + seconds * 1000
  while (Date.now() < endTime) {
    if (hasCommandsInQueue(context)) break  // 有新消息立刻唤醒
    await sleep(1000)
  }
}

// UI 检测 onlySleepToolActive（src/components/REPLWidget.tsx）
const onlySleepToolActive = useMemo(() =>
  inProgressToolUseIDs.every(id => getToolName(id) === SLEEP_TOOL_NAME),
  [messages, inProgressToolUseIDs]
)
// 当 onlySleepToolActive=true 时，spinner 隐藏（不显示"thinking..."）
```

---

## 四、KAIROS 对工具行为的改变

### BashTool — 自动后台化（`src/tools/BashTool/BashTool.tsx`）

```typescript
const ASSISTANT_BLOCKING_BUDGET_MS = 15_000  // 15 秒

// line 976-983：KAIROS 主线程中，命令超过 15s 自动后台化
if (feature('KAIROS') && getKairosActive() && isMainThread && !isBackgroundTasksDisabled && run_in_background !== true) {
  setTimeout(() => {
    if (shellCommand.status === 'running' && backgroundShellId === undefined) {
      assistantAutoBackgrounded = true
      startBackgrounding('tengu_bash_command_assistant_auto_backgrounded')
    }
  }, ASSISTANT_BLOCKING_BUDGET_MS).unref()  // .unref() 防止阻止进程退出
}
// 后台化后模型收到：
// "Command exceeded the assistant-mode blocking budget (15s) and was moved to the background..."
```

### AgentTool — 强制异步（`src/tools/AgentTool/AgentTool.tsx`）

```typescript
// KAIROS 模式下所有子 Agent 强制异步（AgentTool.tsx line 566）
// 注意：读取 AppState.kairosEnabled（运行时 store），而非 bootstrap STATE.kairosActive
const assistantForceAsync = feature('KAIROS') ? appState.kairosEnabled : false
// shouldRunAsync 完整 6 条件（任一即触发）：
// run_in_background || selectedAgent.background || isCoordinator || forceAsync || assistantForceAsync || isProactiveActive()
```

这让主 Agent 成为真正的"协调者"：同时派出多个子 Agent 并行工作，自己继续处理其他任务。

---

## 五、KAIROS_CHANNELS — 外部通知接入（`src/services/mcp/channelNotification.ts`）

### Channel XML 包装格式

```typescript
// SAFE_META_KEY 防注入（line 104）
const SAFE_META_KEY = /^[a-zA-Z_][a-zA-Z0-9_]*$/

// wrapChannelMessage 函数
function wrapChannelMessage(serverName: string, content: string, meta?: Record<string, string>): string {
  const attrs = Object.entries(meta ?? {})
    .filter(([k]) => SAFE_META_KEY.test(k))         // 过滤不合法 key（防注入）
    .map(([k, v]) => ` ${k}="${escapeXmlAttr(v)}"`) // XML 属性转义
    .join('')
  return `<channel source="${escapeXmlAttr(serverName)}"${attrs}>\n${content}\n</channel>`
}
// 结果：<channel source="slack" user="alice">消息内容</channel>
```

### 远程权限审批（Channel Permission Relay）

```typescript
// 常量（channelNotification.ts line 85-86）
export const CHANNEL_PERMISSION_REQUEST_METHOD = 'notifications/claude/channel/permission_request'

// 用户回复格式正则（channelPermissions.ts line 75）
export const PERMISSION_REPLY_RE = /^\s*(y|yes|n|no)\s+([a-km-z]{5})\s*$/i

// 5 字符 requestId 生成（shortRequestId 函数，lines 140-152）
// FNV-1a hash of toolUseID，base-25 编码，字母表去掉 'l'（防混淆）
// 含脏词 blocklist，重哈希最多 10 次
const alphabet = 'abcdefghijkmnopqrstuvwxyz'  // 25 字母
```

**race-with-claim 竞争模式**（`interactiveHandler.ts` lines 316-408）：

```
Claude 想执行高危操作（用户不在终端）
    │
    ├─ 本地权限对话框  ←── race ──► Channel 权限请求推送到用户手机
    ├─ Bridge 审批                    用户回复 "yes abc12"
    ├─ hooks 检查
    └─ ML 分类器
         │
         └─ 第一个 claim() 赢家决定结果，其余忽略
```

**激活条件**（6 重门控）：
1. MCP server 声明 `capabilities.experimental['claude/channel']`
2. Server 在 Anthropic 官方 allowlist
3. 使用 claude.ai OAuth 认证（不支持 API key）
4. 启动参数 `--channels plugin:slack@anthropic`
5. 组织托管设置 `channelsEnabled: true`
6. GrowthBook `tengu_harbor` 开关开启

**激活 channel 后自动禁用**：`EnterPlanMode`/`ExitPlanMode`（无人审批）、`AskUserQuestionTool`（无人回答）

---

## 六、KAIROS_DREAM — 记忆整合

KAIROS 模式下 `getKairosActive() === true` → `autoDream.isGateOpen()` 直接返回 false，禁止自动后台整合。改用磁盘 `/dream` skill（AI 自主决定何时整合）。

### 日期日志系统

每次 KAIROS session 中日期变更时，transcript 片段刷入按日期分类的日志：

```
~/.claude/projects/<cwd>/memory/logs/
  2026/
    04/
      2026-04-01.md    ← 今天的工作记录（格式：HH:MM 时间戳 + 内容摘录）
```

`/dream` skill 整合时优先读日期日志（比穷举 transcript 效率更高）。

### /dream vs autoDream

| | autoDream（普通用户）| KAIROS dream |
|--|---------------------|-------------|
| 触发方式 | 5 重门控自动触发 | AI 自主 / CronCreate 定时 |
| 执行者 | forked agent（后台）| /dream skill（前台）|
| MEMORY.md 上限 | 200 行/25KB | 500 行（dream 提示词中描述）|
| getKairosActive() | N/A | true 时 autoDream 跳过 |

---

## 七、会话持久化（bridge-pointer.json）

**`src/bridge/bridgePointer.ts` 关键常量**：

```typescript
BRIDGE_POINTER_TTL_MS = 4 * 60 * 60 * 1000   // 4 小时（指针有效期）
MAX_WORKTREE_FANOUT   = 50                     // 扫描 worktree 时最多展开 50 个
```

**bridge-pointer.json schema**：

```typescript
{
  sessionId: string,
  environmentId: string,
  source: 'standalone' | 'repl',
}
// KAIROS 持久模式写入 source: 'repl'
// 恢复时只读 source === 'repl' 的指针
```

**hourly mtime 刷新**（`replBridge.ts` lines 1505-1526）：

```typescript
const pointerRefreshTimer = perpetual
  ? setInterval(() => {
      if (reconnectPromise) return  // 重连期间跳过
      void writeBridgePointer(dir, { sessionId, environmentId, source: 'repl' })
    }, 60 * 60_000)  // 每小时更新一次 mtime（防被 4h TTL 清理）
  : null
pointerRefreshTimer?.unref?.()
```

**KAIROS 会话持久化逻辑**：

```
KAIROS 模式退出（perpetual teardown）：
  → 不调用 stopWork / sendResult / closeTransport
  → transport = null（连接断开但 session 保留）
  → bridge-pointer.json 保留（下次 --continue/-c 可恢复）

普通模式退出：
  → 会话标记为 archived，bridge 连接断开
```

---

## 八、全景架构图

```
外部世界                      KAIROS Core                  本地系统
─────────                    ────────────                  ────────

Slack/Telegram  ──MCP──►  KAIROS_CHANNELS
Discord/SMS               wrapChannelMessage()  ◄──────  BashTool
                          SAFE_META_KEY 过滤      自动后台化（15s）
                          Permission Relay
                               │
GitHub Webhook  ──Bridge─►     │
                               ▼              ◄──────────  AgentTool
                          Agent Loop           强制异步      （并发子 Agent）
claude.ai 网页  ──Bridge─► (query.ts)
                               │
                               ▼              ◄──────────  SleepTool
                          SendUserMessage       tick 驱动    1s 轮询唤醒
                          userFacingName()=''
                               │
                               ▼
用户终端/手机   ◄──────────  你看到的消息   ──────────────►  记忆系统
                                                             /dream skill
                                                            日期日志系统
```

---

## 九、与其他系统的关联

| 系统 | 普通模式行为 | KAIROS 模式变化 |
|------|------------|----------------|
| 记忆系统 | autoDream 自动后台 | autoDream 跳过（`getKairosActive()=true`），改用 /dream skill |
| 任务系统 | LocalAgentTask 可同步 | `assistantForceAsync=true`，全部强制异步 |
| Bridge | 退出时 archive | perpetual teardown：保留 bridge-pointer，支持 --continue |
| Analytics | 普通标签 | 额外标记 `kairosActive: true` |
| Status Bar | 正常显示 | 完全隐藏 |
| 计划模式 | 正常可用 | 有 channel 时禁用 |
| 提问工具 | 正常可用 | 有 channel 时禁用 |
| BashTool 超时 | 无自动后台 | 15s 后自动后台（ASSISTANT_BLOCKING_BUDGET_MS）|
| Session Memory | token 阈值触发 | 同普通模式（仍然有 Session Memory）|

---

## 关键文件

| 文件 | 内容 |
|------|------|
| `src/bootstrap/state.ts` | `kairosActive` Atom 定义（line 72）|
| `src/main.tsx` | KAIROS 激活判断（lines 1054-1094，4 重门控）|
| `src/assistant/index.ts` | `isAssistantMode()`，环境变量 + CLI flag 读取 |
| `src/tools/BriefTool/BriefTool.ts` | SendUserMessage 完整实现（userFacingName/isConcurrencySafe/mapToolResult）|
| `src/tools/BriefTool/prompt.ts` | BRIEF_TOOL_NAME / LEGACY_BRIEF_TOOL_NAME 常量 |
| `src/proactive/index.ts` | 自主循环 3 状态 + 5 控制函数 |
| `src/tools/SleepTool/` | Sleep 实现（1s 轮询、onlySleepToolActive UI 检测）|
| `src/services/autoDream/autoDream.ts` | `isGateOpen()`（kairosActive 时返回 false）|
| `src/services/mcp/channelNotification.ts` | Channel XML 包装（wrapChannelMessage、SAFE_META_KEY）|
| `src/services/mcp/channelPermissions.ts` | Permission Relay（CHANNEL_PERMISSION_REQUEST_METHOD、shortRequestId）|
| `src/services/mcp/interactiveHandler.ts` | race-with-claim 竞争模式（lines 316-408）|
| `src/bridge/bridgePointer.ts` | bridge-pointer 常量（TTL_MS、MAX_WORKTREE_FANOUT）|
| `src/bridge/initReplBridge.ts` | perpetual teardown + hourly mtime 刷新 |
| `src/tools/BashTool/BashTool.tsx` | ASSISTANT_BLOCKING_BUDGET_MS（15s）+ .unref() |
| `src/tools/AgentTool/AgentTool.tsx` | `assistantForceAsync = feature('KAIROS') ? appState.kairosEnabled : false` |
| `src/constants/prompts.ts` | `getBriefSection()`, `getProactiveSection()` 提示词 |
