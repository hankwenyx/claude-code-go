# KAIROS — AI 助手模式深度解读

KAIROS 是 Claude Code 内部代号为"助手模式"的高级功能体系，将 Claude Code 从一个"等待你输入才行动"的编码工具，升级为一个**持续运行、主动决策、能与外部世界双向通信**的个人 AI 助手。

> "KAIROS" 源自希腊语，意为"恰当的时机"——呼应其核心能力：AI 在合适的时机主动行动，而不是被动等待。

目前 KAIROS 仅面向 Anthropic 内部用户（`USER_TYPE=ant`），但其子功能正在逐步对外开放。

---

## 普通模式 vs KAIROS 模式

在理解 KAIROS 之前，先看看它改变了什么：

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
| 驱动方式 | 用户输入驱动 | 定时 tick + 外部通知驱动 |
| 消息输出 | 直接写到终端 | 必须通过 `SendUserMessage` 工具 |
| 界面形态 | 完整对话记录 | 精简 chat 视图（隐藏工具调用细节） |
| 长时命令 | 阻塞等待完成 | 自动后台化，Claude 继续协调 |
| 子 Agent | 可同步可异步 | 强制异步（主 Agent 不阻塞） |
| 外部通知 | 只能看终端 | Slack/Telegram/Discord 等推送 |
| 记忆整合 | 自动后台触发 | 磁盘 `/dream` skill 驱动 |
| 计划模式 | 正常可用 | 有 channel 时禁用（无人守键盘）|

---

## KAIROS 的子功能体系

KAIROS 被拆分为多个可独立开关的子 feature flag：

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

## 一、KAIROS_BRIEF — 消息通道

### 为什么需要这个？

在 KAIROS 模式下，Claude 可能花几小时在后台处理任务。这段时间你不会盯着终端，对话记录里可能有几百条工具调用。如果 Claude 直接把最终结论写在 transcript 里，你根本找不到。

`SendUserMessage`（内部也叫 `Brief`）工具解决了这个问题：**只有 Claude 主动通过这个工具发送的内容，才会出现在你的"聊天视图"里**。其他所有工具调用和中间过程，都折叠在"详情"里，默认不展示。

### 工作原理

```
Claude 完成一个任务后：
  ╔═══════════════════════════════════════╗
  ║  SendUserMessage({                    ║
  ║    message: "## 安全审查完成\n\n..."  ║
  ║    attachments: ["security-report.md"]║
  ║    status: "normal"                   ║  ← 回复用户
  ║  })                                   ║
  ╚═══════════════════════════════════════╝

你看到的：
  ┌─────────────────────────────────────┐
  │ [Claude]  ## 安全审查完成           │  ← 只显示这条
  │ 发现 3 个潜在漏洞...                │
  └─────────────────────────────────────┘

  [展开详情] ← 几百条工具调用折叠在这里
```

`status` 字段的两种值：
- `normal`：回复用户的消息（对话式）
- `proactive`：主动发起的消息，比如"后台任务完成了"

### 如何激活 Brief 模式

```
方式 1：启动时加 --brief 参数
方式 2：settings.json 设置 defaultView: 'chat'
方式 3：运行 /brief 斜杠命令（需 GrowthBook 开关）
方式 4：在 /config 设置界面选 defaultView
方式 5：CLAUDE_CODE_BRIEF 环境变量（测试用）
方式 6：kairosActive=true 时自动激活
```

激活后，系统提示词会注入这段指令：

> *"SendUserMessage is where your replies go. Text outside it is visible if the user expands the detail view, but most won't — assume unread."*

翻译：你的回复只有通过 `SendUserMessage` 才能被用户看到。其他输出用户基本不看，当作不存在。

---

## 二、PROACTIVE — 自主循环引擎

PROACTIVE 是 KAIROS 的运行引擎，让 Claude 不再等待输入，而是自主运转。

### tick 驱动机制

```
KAIROS 启动
    │
    ▼
 进入自主循环
    │
    ├─ 收到外部通知（Channel/用户输入）→ 立刻处理
    │
    └─ 没有输入 → Claude 调用 Sleep 工具等待
                       │
                       └─ 等待期间：
                           ├─ 每秒检查 hasCommandsInQueue()
                           ├─ 收到通知 → 中断睡眠，立刻处理
                           └─ 等待结束 → 下一次 tick，自主决定做什么
```

系统提示词中关于自主模式的关键指令：

```
# Autonomous work

You are running autonomously. You will receive `<tick>` prompts...

## Pacing
Use the Sleep tool to control how long you wait.
If nothing to do on a tick, you MUST call Sleep.

## Terminal focus
- Unfocused（用户不在终端）: 主动行动，能做就做
- Focused（用户在终端）: 更多协作，主动询问

## Talking to the user
SendUserMessage is where your replies go...
```

### 暂停与恢复

```
用户按 Ctrl+C → pauseProactive()    Claude 暂停自主循环
用户提交输入  → resumeProactive()   Claude 继续自主循环

API 错误时   → contextBlocked=true  等待 compact 边界后清除
```

### SleepTool

自主模式专属工具，Claude 用它控制自己的"休眠时长"：

```
Claude 没事做了 → Sleep({ seconds: 300 })
    │
    └─ 5 分钟后醒来，或者：
        ├─ 有 Channel 消息推进来
        ├─ 用户在终端输入了什么
        └─ 定时任务（CronCreate）触发
```

---

## 三、KAIROS_CHANNELS — 外部通知接入

### 这是什么？

让 Slack、Telegram、Discord、SMS 等作为 MCP server，实时向 Claude 推送消息。Claude 可以：
- 通过 Channel 的工具回复消息
- 用 `SendUserMessage` 通知本地用户
- 两者都用（"帮你转发"）

```
用户在 Slack 发消息
    │ MCP channel 协议
    ▼
消息被包装为：
  <channel source="slack" user="alice">
    嘿，帮我看一下这个 PR 的安全性
  </channel>
    │
    ▼
注入到 Claude 的对话中
    │
    ▼
Claude 分析 PR → 通过 Slack MCP tool 回复 Alice
             → 同时用 SendUserMessage 告知本地用户
```

### 远程权限审批（Channel Permission Relay）

这是 KAIROS_CHANNELS 最精妙的设计之一：

```
Claude 想执行一个高危操作（需要用户确认）
但是... 用户不在终端！

    ↓ 有 channel 连接

权限请求通过 channel 发到用户手机/桌面：
  "Claude 想要执行 bash rm -rf build/，输入 'yes abc123' 确认"

用户回复 "yes abc123"
    ↓
Claude 收到确认，继续执行
```

**安全门控（非常严格）：**

```
MCP server 必须：
  1. 声明 capabilities.experimental['claude/channel'] 能力
  2. 在 Anthropic 官方 allowlist 中
  3. 使用 claude.ai OAuth 认证（不支持 API key）

Session 必须：
  4. 显式声明 --channels plugin:slack@anthropic
  5. 组织在托管设置中启用 channelsEnabled: true
  6. GrowthBook tengu_harbor 开关开启
```

**激活 channel 后，这些工具被自动禁用：**
- `EnterPlanMode` / `ExitPlanMode`（无人审批计划）
- `AskUserQuestionTool`（无人在终端回答）

---

## 四、KAIROS_DREAM — 记忆整合

KAIROS 模式的记忆系统与普通用户不同——它不用 autoDream 的自动后台触发，而是通过磁盘上的 `/dream` skill 手动或定时触发，给 AI 更多控制权。

### dream 的四个阶段

```
/dream 技能触发（或 Cron 定时触发）
    │
    ▼
Phase 1 Orient：了解现状
  → 读取 ~/.claude/projects/memory/ 目录
  → 读取 MEMORY.md 索引，看有哪些已有主题

Phase 2 Gather：收集新信息
  → 优先读取 logs/YYYY/MM/YYYY-MM-DD.md（日期日志）
  → 也 grep transcript 补充

Phase 3 Consolidate：整合更新
  → 合并新信息到 topic 文件
  → 修正过时或错误的记忆
  → 删除重复内容

Phase 4 Prune & Index：整理索引
  → 更新 MEMORY.md 入口文件
  → 保持在 25KB / 500 行以内
```

### 日期日志系统

KAIROS 模式专属：每次日期变更时，会把当天的 transcript 刷入按日期分类的日志文件：

```
~/.claude/projects/<cwd>/memory/logs/
  2026/
    03/
      2026-03-28.md    ← 3月28日的工作记录
      2026-03-29.md
    04/
      2026-04-01.md    ← 今天
```

这让 `/dream` 在整合时可以"回溯历史"，知道哪天做了什么。

### 普通用户 autoDream vs KAIROS dream

```
普通用户 autoDream：
  时间门 + 会话数门 → 自动后台触发 → forked agent 处理
  （自动，无需干预）

KAIROS dream：
  getKairosActive() === true → autoDream 直接返回 false，不触发
  → 改用磁盘 skill，由 AI 自主决定何时 dream
  → 可以通过 CronCreate 设置定时 dream（比如每天凌晨）
```

---

## 五、KAIROS 对工具行为的改变

KAIROS 激活后，多个工具的行为会静默改变：

### BashTool — 自动后台化

```typescript
// BashTool.tsx 中的逻辑
if (kairosActive && isMainThread && !isBackgroundTasksDisabled) {
  setTimeout(() => {
    if (命令还在运行中) {
      startBackgrounding()  // 自动转后台
    }
  }, ASSISTANT_BLOCKING_BUDGET_MS)  // 超时阈值
}
```

在 KAIROS 模式下，长时间命令不会阻塞主 Agent——超过阈值自动转为后台任务，Claude 继续协调其他工作，用 `TaskOutput` 随时查看进度。

### AgentTool — 强制异步

```typescript
// AgentTool.tsx
const assistantForceAsync = kairosActive ? true : false
// 所有子 Agent 都以后台模式启动，主 Agent 不等待
```

这让 KAIROS 下的主 Agent 成为真正的"协调者"，可以同时派出多个子 Agent 并行工作，自己继续处理其他任务。

### 规律总结

```
KAIROS 下工具的统一原则：
  → 不要让主 Agent 阻塞等待
  → 能后台的都后台
  → 结果通过通知机制返回
```

---

## 六、启动与激活流程

```
main.tsx 初始化
    │
    ├─ feature('KAIROS') 检查（编译时 flag）
    │
    ├─ assistantModule.isAssistantMode()
    │     ├─ CLAUDE_CODE_ASSISTANT_MODE=1 环境变量
    │     └─ --assistant CLI flag
    │
    ├─ kairosGate.isKairosEnabled()
    │     ├─ GrowthBook tengu_kairos gate
    │     └─ --assistant flag（跳过 GrowthBook，强制启用）
    │
    ├─ checkHasTrustDialogAccepted()  ← 目录必须被用户信任
    │     （防止恶意 repo 的 assistant.md 注入系统提示词）
    │
    ├─ setKairosActive(true)           设置全局状态
    ├─ opts.brief = true               自动启用 SendUserMessage
    └─ assistantModule.initializeAssistantTeam()  初始化助手团队
```

---

## 七、会话持久化

KAIROS 下的会话与普通模式不同——它**不会在退出时关闭**，保持长期存活：

```
普通模式退出：
  → 会话标记为 archived，bridge 连接断开

KAIROS 模式退出：
  → bridge-pointer.json 保留（记录 session_id）
  → 下次启动时通过 --continue/-c 恢复到同一 session
  → 连续性保持：历史消息、记忆、任务状态全部延续
```

```
claude assistant <sessionId>
  → 以"纯 viewer 模式"附加到远程运行的 session
  → 本地只负责显示，agent 循环在远程运行
  → 通过 /api/sessions/{id}/events 懒加载历史
```

---

## 八、全景架构图

```
外部世界                      KAIROS Core                  本地系统
─────────                    ────────────                  ────────

Slack/Telegram  ──MCP──►  KAIROS_CHANNELS
Discord/SMS               消息包装
                          权限代理         ◄──────────────  BashTool
                               │           自动后台化       （后台执行）
GitHub Webhook  ──Bridge─►     │
                               ▼           ◄──────────────  AgentTool
                          Agent Loop        强制异步         （并行子 Agent）
claude.ai 网页  ──Bridge─► (query.ts)
                               │
                               ▼           ◄──────────────  SleepTool
                          SendUserMessage    tick 驱动       （等待/唤醒）
                          (KAIROS_BRIEF)
                               │
                               ▼
用户终端/手机   ◄──────────  你看到的消息   ──────────────►  记忆系统
                                                             /dream skill
                                                            (KAIROS_DREAM)
```

---

## 九、与其他系统的关联

| 系统 | 普通模式行为 | KAIROS 模式变化 |
|------|------------|----------------|
| 记忆系统 | autoDream 自动后台 | autoDream 跳过，改用 /dream skill |
| 任务系统 | LocalAgentTask 可同步 | 全部强制异步 |
| Bridge | 退出时 archive | 退出时保留，支持 --continue |
| Analytics | 普通标签 | 额外标记 `kairosActive: true` |
| Status Bar | 正常显示 | 完全隐藏 |
| 计划模式 | 正常可用 | 有 channel 时禁用 |
| 提问工具 | 正常可用 | 有 channel 时禁用 |

---

## 关键文件

| 文件 | 内容 |
|------|------|
| `src/bootstrap/state.ts` | `kairosActive` 全局状态定义 |
| `src/main.tsx` | KAIROS 激活判断逻辑 |
| `src/assistant/index.ts` | isAssistantMode()，读取环境变量 |
| `src/tools/BriefTool/BriefTool.ts` | SendUserMessage 实现，isBriefEnabled() |
| `src/proactive/index.ts` | 自主循环的 active/paused 状态管理 |
| `src/services/autoDream/autoDream.ts` | autoDream 门控（kairosActive 时跳过） |
| `src/services/autoDream/consolidationPrompt.ts` | 四阶段记忆整合提示词 |
| `src/tasks/DreamTask/DreamTask.ts` | dream 进度的 UI 任务状态 |
| `src/services/mcp/channelNotification.ts` | Channel 门控与消息包装 |
| `src/constants/prompts.ts` | getBriefSection(), getProactiveSection() |
| `src/tools/BashTool/BashTool.tsx` | 自动后台化逻辑 |
| `src/tools/AgentTool/AgentTool.tsx` | 强制异步逻辑 |
| `src/bridge/initReplBridge.ts` | KAIROS 的持久化 bridge 会话 |
