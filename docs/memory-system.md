# 记忆系统

Claude Code 的记忆系统解决一个核心问题：**AI 怎么在多次对话之间"记住"东西？**

不同类型的记忆有不同的生命周期——有的只活在本次会话，有的永久保存，有的可以跨团队共享。

---

## 记忆的六个层级

```
                    优先级（高）
                        ▲
                        │
  ┌─────────────────────┴───────────────────────────┐
  │  Session Memory    会话笔记（本次会话内）          │ ← 最"新鲜"
  ├─────────────────────────────────────────────────┤
  │  TeamMem          团队共享记忆（多人同步）         │
  ├─────────────────────────────────────────────────┤
  │  AutoMem          自动跨会话记忆（个人）           │
  ├─────────────────────────────────────────────────┤
  │  Local            本地私有规则（不提交 git）       │
  ├─────────────────────────────────────────────────┤
  │  Project          项目规则（提交到代码库）         │
  ├─────────────────────────────────────────────────┤
  │  User             全局个人偏好                    │
  ├─────────────────────────────────────────────────┤
  │  Managed          企业管理员统一策略               │ ← 最"权威"
  └─────────────────────────────────────────────────┘
                        │
                    优先级（低）
```

"后加载的优先级更高"——靠近你项目的设置会覆盖全局设置。

---

## CLAUDE.md — 最常用的记忆方式

CLAUDE.md 就是写给 AI 看的"项目说明书"。每次对话开始时，它的内容会被注入到系统提示词中。

### 加载顺序（低优先级 → 高优先级）

```
1. Managed   /etc/claude-code/CLAUDE.md              企业管理员统一设置（IT 管理员写）
             /etc/claude-code/.claude/rules/*.md      始终加载，不可被用户禁用

2. User      ~/.claude/CLAUDE.md                     你自己的全局偏好
             ~/.claude/rules/*.md
             （允许 @include 外部文件）

3. Project   从根目录 → 向 cwd 方向逐层加载：
             {dir}/CLAUDE.md                         提交到 git，团队共享
             {dir}/.claude/CLAUDE.md
             {dir}/.claude/rules/*.md
             → 越靠近 cwd 的文件越后加载，优先级越高

4. Local     {dir}/CLAUDE.local.md                   gitignored，你对项目的私人笔记

5. AutoMem   ~/.claude/projects/{slug}/memory/MEMORY.md
             截断：最多 200 行 / 25KB

6. TeamMem   团队共享记忆目录的 MEMORY.md（feature('TEAMMEM') 时）
```

### 加载时的处理细节

- 目录遍历：从 `getOriginalCwd()` 向上到根目录，再**反向**（根 → cwd）依次处理
- worktree 嵌套：跳过 canonicalRoot 内但 gitRoot 外的 checked-in 文件
- 块级 HTML 注释（`<!-- ... -->`）被剥除，内联注释和代码块内注释保留
- `claudeMdExcludes` glob 模式可排除部分文件（仅对 User/Project/Local 类型有效）

### `@include` 指令

```
@path               相对路径
@./relative/path    相对路径
@~/home/path        home 目录展开
@/absolute/path     绝对路径
```

- 用 marked Lexer 解析，跳过代码块中的 `@`
- 最大嵌套深度：5 层（`MAX_INCLUDE_DEPTH`）
- 循环引用防护：`processedPaths Set`
- 二进制文件扩展名不允许（白名单 `TEXT_FILE_EXTENSIONS`）

---

## AutoMem — AI 自动帮你记

AutoMem 是最有趣的记忆机制。**你不需要手动操作**，Claude 会在每次对话结束后，悄悄在后台把值得记录的东西整理好，下次对话时自动带上。

### 存在哪里？

```
~/.claude/projects/<git-canonical-root>/memory/
  MEMORY.md              ← 索引文件（最多 200 行，25KB）
  user_role.md           ← 关于你是谁、你做什么
  project_context.md     ← 项目上下文
  feedback_testing.md    ← 你对 AI 行为的反馈
  references.md          ← 外部系统指针
  ...（每个主题一个文件）
```

多个 git worktree 共享同一个 memory 目录（因为使用 canonical git root）。

### AI 会记什么？（四种类型）

**1. `user` 类型（用户画像）**
- 记录：角色、目标、职责、知识背景
- 触发：用户自我介绍（"我是数据科学家"、"我用 Go 十年了"）
- scope：**始终为 private**（不跨用户共享）

**2. `feedback` 类型（行为指导）**
- 记录：用户对 AI 行为的纠正/认可
- 触发：负向纠正（"不要这样"）或正向确认（"是的，就这样"）
- 格式：规则本体 + **Why:** 行 + **How to apply:** 行
- scope：项目级约定升级为 team，个人偏好保持 private

**3. `project` 类型（项目上下文）**
- 记录：不能从代码/git 历史推导的工作状态、目标、Bug、事件
- 特别规则：**相对日期必须转为绝对日期**（"周四" → "2026-03-05"）
- scope：强烈建议选 team

**4. `reference` 类型（外部资源指针）**
- 记录：Linear 项目、Slack 频道、Grafana 面板等位置
- scope：通常为 team

**什么不应保存**（WHAT_NOT_TO_SAVE_SECTION 明确排除）：
- 代码模式、架构、文件路径（读代码可得）
- Git 历史（`git log` 是权威来源）
- 调试解决方案（fix 在代码里）
- CLAUDE.md 中已文档化的内容
- 临时任务细节、当前会话状态

### 两步写入流程

**第一步**：写 topic 文件（frontmatter 格式）

```markdown
---
name: {{memory name}}
description: {{one-line description — 用于判断未来对话的相关性}}
type: {{user, feedback, project, reference}}
---

{{memory content}}
（feedback/project 类型附加 **Why:** 和 **How to apply:** 行）
```

**第二步**：更新索引 `MEMORY.md`

```
- [Title](file.md) — one-line hook
```

约束：`MEMORY.md` 是索引，不是内容本体；每条目一行；不得直接写内容。

功能开关 `tengu_moth_copse` 可跳过第二步（`skipIndex = true`，直接单步写文件）。

### 何时触发提取？（forked agent 执行）

每次主 Agent 完成一轮对话（最终回复无工具调用），在后台启动 forked agent：

```
Turn 1：并行发出所有 FileRead 调用（读取所有可能更新的文件）
Turn 2：并行发出所有 Write/Edit 调用
（最多 5 轮，maxTurns 限制）
```

**互斥逻辑**：`hasMemoryWritesSince(messages, sinceUuid)` 检测主 Agent 是否已手动写过记忆文件。若已写入，forked agent 跳过，确保不覆盖用户的手动编辑。

### forked agent 的权限限制（`createAutoMemCanUseTool`）

```
FileRead / Grep / Glob → 无条件允许（只读工具）
Bash                   → 仅允许 isReadOnly() 的命令（ls/find/grep/cat 等）
FileEdit / FileWrite   → 仅允许路径前缀匹配 getAutoMemPath() 的操作
其他工具               → deny 并记录 analytics（tengu_auto_mem_tool_denied）
```

---

## Session Memory — 会话内的"工作笔记"

Session Memory 是 Claude 在当前会话里记的"工作笔记"，主要作用是：**在对话被压缩后，保持任务的连续性。**

### 触发条件（`shouldExtractMemory`）

```typescript
// 初始化门：首次触发
totalContextTokens >= minimumMessageTokensToInit  // 默认 10,000

// 更新门（token 阈值始终必须满足）：
hasMetTokenThreshold = (currentTokens - tokensAtLastExtraction) >= 5,000
hasMetToolCallThreshold = toolCallsSinceLastUpdate >= 3
hasToolCallsInLastTurn = 最后一条 assistant 消息中有 tool_use

触发条件：
  (hasMetTokenThreshold && hasMetToolCallThreshold)    ← 两个门都通过
  OR
  (hasMetTokenThreshold && !hasToolCallsInLastTurn)    ← token 门 + 自然对话断点
```

GrowthBook `tengu_sm_config` 可动态覆盖所有阈值。
仅主线程触发（`querySource === 'repl_main_thread'`），subagent 不触发。

### 笔记的结构（DEFAULT_SESSION_MEMORY_TEMPLATE，9 章）

| 章节 | 作用 |
|------|------|
| # Session Title | 5-10 词的紧凑描述性标题 |
| # Current State | **当前活跃工作、未完成任务、下一步行动**（压缩后最重要的延续锚点！）|
| # Task Specification | 用户要求构建什么、设计决策 |
| # Files and Functions | 重要文件及其作用 |
| # Workflow | 通常运行的命令及其顺序和输出解释 |
| # Errors & Corrections | 遇到的错误、失败的方法（禁止重试）|
| # Codebase and System Documentation | 重要系统组件及其工作方式 |
| # Learnings | 有效/无效的方法，避免事项 |
| # Key Results | 用户要求的精确输出（表格、答案等完整复制）|
| # Worklog | 每步操作的简短流水记录 |

每章节上限：2,000 tokens；总体上限：12,000 tokens。
超出时提示模型紧缩，优先保留 "Current State" 和 "Errors & Corrections"。

### Session Memory 的权限更严格

`createMemoryFileCanUseTool(memoryPath)` 只允许 `FileEdit` 对**精确匹配** `memoryPath` 的文件操作，所有其他工具和路径均被拒绝。

---

## AutoDream — 定期整理记忆

随着时间推移，AutoMem 里的内容可能变得碎片化或过时。AutoDream 负责定期"大扫除"，将零散记录整合成结构化主题文件。

### 触发条件（四重门控）

**门 1：总开关 `isGateOpen()`**

```typescript
if (getKairosActive()) return false    // KAIROS 模式使用 disk-skill dream
if (getIsRemoteMode()) return false    // 远程模式禁止
if (!isAutoMemoryEnabled()) return false
return isAutoDreamEnabled()            // GrowthBook tengu_onyx_plover 或 settings 字段
```

**门 2：时间门（Time Gate）**

```
读取 .consolidate-lock 文件的 mtime 作为 lastConsolidatedAt
条件：(Date.now() - lastConsolidatedAt) / 3_600_000 >= cfg.minHours
默认阈值：minHours = 24（可通过 GrowthBook 配置）
```

**门 3：扫描节流（Scan Throttle）**

```
时间门通过但 session 门未通过时，防止每轮都扫描
间隔：SESSION_SCAN_INTERVAL_MS = 10 分钟
```

**门 4：会话数门（Session Gate）**

```
扫描 getProjectDir(cwd) 下所有 session transcript 文件
过滤：mtime > lastConsolidatedAt，排除当前 session，排除 agent-*.jsonl
条件：sessionIds.length >= cfg.minSessions（默认 5）
```

**门 5：锁门（Lock Gate）**

防止多进程并发整合：
```
tryAcquireConsolidationLock()：
  读取 .consolidate-lock（PID + mtime）
  持有者进程仍存活（isProcessRunning(holderPid)）→ 跳过
  锁超过 HOLDER_STALE_MS = 1小时 → 视为过期，可重新申领
  写 PID 后再次读回验证（防竞态：最后写入者赢）
```

**回滚机制**：执行失败时 `rollbackConsolidationLock(priorMtime)` 将 mtime 回退，让时间门下次可再次通过。10 分钟退避防止频繁重试。

### 四阶段实现（`consolidationPrompt.ts`）

```
Phase 1 Orient：了解现状
  → ls memory 目录，读取 MEMORY.md 索引
  → 浏览已有 topic 文件（防止创建重复）
  → 如有 logs/ 子目录，检查近期条目

Phase 2 Gather：采集新信号
  来源优先级：
  1. 每日日志文件 logs/YYYY/MM/YYYY-MM-DD.md（若存在）
  2. 已有记忆中的漂移（与当前代码库状态矛盾的事实）
  3. Transcript 搜索（最后手段，窄搜索）：
     grep -rn "<narrow term>" ${transcriptDir}/ --include="*.jsonl" | tail -50

  明确禁止：不得穷举读取 transcript，仅针对已知内容搜索

Phase 3 Consolidate：整合
  → 合并新信号到现有 topic 文件（而非创建近似重复）
  → 相对日期转绝对日期
  → 删除被证伪的事实
  → 参照 auto-memory 章节的类型约定

Phase 4 Prune & Index：修剪与重建索引
  → 更新 MEMORY.md
  → 保持 ≤200 行 且 ≤25KB
  → 移除过时条目指针
  → 缩短冗长行（>200 字符说明内容应在 topic 文件）
  → 解决两个文件间的矛盾
```

---

## TeamMem — 团队共享记忆

多人协作时，团队成员可以共享项目记忆。底层通过 HTTP API + 文件系统 + 防抖推送实现。

### 同步语义（API 合同）

```
GET  /api/claude_code/team_memory?repo={owner/repo}           → 完整数据（含 checksums）
GET  /api/claude_code/team_memory?repo={owner/repo}&view=hashes → 仅元数据（不含内容体）
PUT  /api/claude_code/team_memory?repo={owner/repo}           → 上传条目（upsert 语义）
404  → 服务端暂无数据
```

### Server-wins Pull 逻辑

```
GET 服务端数据（If-None-Match 条件请求）
  304 → 直接返回（缓存命中）
  200 → 遍历 server entries：
    ├─ 比较本地内容 SHA256，相同则跳过（保留 mtime，不触发 watcher）
    └─ 不同则写入本地

注意：删除不传播！服务端没有的文件，本地不会被删除
```

### 增量 Push 逻辑（Delta Upload）

```
读取所有本地文件 → 计算 sha256:<hex> 哈希
与 serverChecksums 比较 → 只上传 hash 变化的文件
按 200KB 分批（MAX_PUT_BODY_BYTES，防 413）
PUT 带 If-Match 乐观锁
  412 冲突 → fetchTeamMemoryHashes（轻量 GET ?view=hashes）刷新 checksums
  最多重试 MAX_CONFLICT_RETRIES = 2 次
```

### Watcher 机制

- 防抖 2秒（`DEBOUNCE_MS = 2000`）：最后一次文件变化后等 2s 再推送
- fs.watch `{recursive: true}`（非 chokidar，避免每文件占用 fd）
- 永久失败抑制（`pushSuppressedReason`）：no_oauth、no_repo、4xx（除 409/429）→ 永久停止重试，直到文件 ENOENT 或会话重启

### Secret 安全守卫

上传前调用 `scanForSecrets(content)`（基于 gitleaks 规则），含密钥文件跳过上传，记录到 `skippedSecrets`。

---

## 各类记忆的比较

| | CLAUDE.md | AutoMem | Session Memory | TeamMem |
|--|-----------|---------|----------------|---------|
| **谁来写** | 你（手动）| AI 自动 | AI 自动 | AI 自动（Push）|
| **跨会话** | ✓ | ✓ | ✗ | ✓ |
| **跨用户** | 项目级可以 | ✗ | ✗ | ✓（团队）|
| **需要操作** | 手动编辑 | 无需 | 无需 | 无需 |
| **存储位置** | 项目/用户目录 | `~/.claude/projects/` | 同左 | 服务端 + 本地 |
| **触发方式** | 启动时注入 | 对话结束后 forked agent | token 阈值 | 文件变化后推送 |

---

## 设计哲学

**为什么所有记忆写入都是后台非阻塞的？**
记忆提取本身需要 AI 处理，如果同步执行会让用户等待。通过 forked agent 在后台运行，主对话完全不受影响。

**为什么 forked agent 权限受限（只能写 memory 目录）？**
安全考虑——记忆提取的 agent 没必要有读写任意文件的权限，最小化权限原则。

**为什么 AutoMem 路径使用 canonical git root 而不是 cwd？**
多个 git worktree 共享同一个代码库，它们应该共享同一份记忆，而不是各自维护一份。

**为什么 KAIROS 模式跳过 autoDream？**
KAIROS 是长期存活的 session，AI 自己能决定何时 dream，不需要自动触发机制。

---

## 关键文件

| 文件 | 里面有什么 |
|------|-----------|
| `src/utils/claudemd.ts` | CLAUDE.md 发现、加载、`@include` 解析 |
| `src/memdir/memoryTypes.ts` | 四种记忆类型定义（user/feedback/project/reference）|
| `src/memdir/memdir.ts` | `loadMemoryPrompt()`、MEMORY.md 截断（200行/25KB）|
| `src/services/extractMemories/prompts.ts` | AutoMem 两步写入提示词 |
| `src/services/SessionMemory/prompts.ts` | 9 章模板、触发阈值 SessionMemoryConfig |
| `src/services/SessionMemory/sessionMemoryUtils.ts` | `shouldExtractMemory()` 判断逻辑 |
| `src/services/autoDream/autoDream.ts` | AutoDream 四重门控、锁机制、回滚 |
| `src/services/autoDream/consolidationPrompt.ts` | 四阶段整合提示词 |
| `src/services/teamMemorySync/index.ts` | Pull/Push/Delta 逻辑 |
| `src/services/teamMemorySync/watcher.ts` | 文件 watcher 防抖与永久失败抑制 |
| `src/utils/forkedAgent.ts` | forked agent 执行（共享 prompt cache + 权限隔离）|
