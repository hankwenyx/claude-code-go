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

### 文件在哪里？

```
/etc/claude-code/CLAUDE.md         企业统一设置（IT 管理员写）
~/.claude/CLAUDE.md                你自己的全局偏好（"我喜欢用 TypeScript"）
project-root/CLAUDE.md             项目说明（提交到 git，团队共享）
project-root/.claude/rules/*.md   细粒度规则文件（可以按文件路径过滤）
project-root/CLAUDE.local.md       你对这个项目的私人笔记（gitignore）
```

### 加载时怎么处理？

```
从根目录 → 向 cwd 方向逐层加载
  └─ 靠近 cwd 的文件，后加载，优先级更高

每个文件还可以用 @include 引用其他文件：
  @include ./shared-rules.md   ← 支持嵌套，最多 5 层
```

### 注入格式

进入系统 Prompt 时，前面会加上：

> **"以下内容是代码库和用户指令，IMPORTANT：这些指令会覆盖默认行为，你必须严格遵守。"**

然后每个文件的内容附上来源说明：
```
Contents of /path/to/CLAUDE.md (project instructions, checked into the codebase):
<内容>

Contents of ~/.claude/CLAUDE.md (user's private global instructions):
<内容>
```

---

## AutoMem — AI 自动帮你记

AutoMem 是最有趣的记忆机制。**你不需要手动操作**，Claude 会在每次对话结束后，悄悄在后台把值得记录的东西整理好，下次对话时自动带上。

### 存在哪里？

```
~/.claude/projects/<git-root>/memory/
  MEMORY.md        ← 索引文件，记录所有主题（最多 200 行）
  user_role.md     ← 关于你是谁、你做什么的记忆
  project_goals.md ← 关于这个项目的记忆
  coding_prefs.md  ← 你的编码风格偏好
  ...（每个主题一个文件）
```

多个 git worktree 共享同一个 memory 目录（因为用的是 canonical git root）。

### AI 会记什么？

记忆分四类：

```
user     → 关于你这个人：角色、目标、技能背景
           例："用户是一个有 10 年经验的后端工程师，专注于 Go"

feedback → 你对 AI 行为的纠正/认可
           例："用户说过不要在注释里写废话"
           包含：规则 + Why + How to apply 三段结构

project  → 项目相关的上下文（代码看不出来的信息）
           例："这个项目的认证用的是内部 SSO，不是标准 OAuth"

reference → 外部系统的链接/入口
           例："日志看 Grafana: https://..."
```

### 什么时候触发提取？

```
每次你跟 Claude 结束一轮对话（Claude 最终回复没有工具调用）
  │
  ├─ 你自己直接写过 AutoMem？→ 跳过（避免覆盖你的内容）
  │
  └─ 在后台启动一个独立的子 Agent：
       ├─ Turn 1：并行读取现有记忆文件
       └─ Turn 2：并行写入更新
       （最多 5 轮，权限严格限制：只能写 memory 目录）
```

### 两步写入（防止并发冲突）

```
Step 1: 写 topic 文件
        ~/.claude/projects/.../memory/user_role.md  ← 内容更新

Step 2: 更新索引
        MEMORY.md 追加一行：
        - [用户角色](user_role.md) — 资深 Go 工程师，专注后端
```

---

## Session Memory — 会话内的"工作笔记"

Session Memory 是 Claude 在当前会话里记的"工作笔记"，主要作用是：**在对话被压缩后，保持任务的连续性。**

### 触发条件

```
对话达到一定量（同时满足）：
  ├─ token 总数 ≥ 10,000
  ├─ 距上次记录新增 ≥ 5,000 tokens
  └─ 工具调用次数 ≥ 3 次
        │
        ▼
  在后台悄悄更新笔记文件
```

### 存在哪里？

```
~/.claude/projects/<cwd>/<sessionId>/session-memory/summary.md
```

### 笔记的结构（9 个固定章节）

```markdown
# Session Title          ← 会话标题
# Current State          ← 当前进展（压缩后最重要的延续锚点！）
# Task Specification     ← 任务描述
# Files and Functions    ← 操作过的文件
# Workflow               ← 工作流程
# Errors & Corrections   ← 遇到的问题
# Codebase Documentation ← 代码库知识
# Learnings              ← 学到的东西
# Key Results            ← 关键结论
# Worklog                ← 操作日志
```

每章节上限 2,000 tokens，总体上限 12,000 tokens。

---

## AutoDream — 定期整理记忆

随着时间推移，AutoMem 里的内容可能变得碎片化或过时。AutoDream 负责定期"大扫除"。

### 触发条件

```
同时满足：
  ├─ 距上次整合 ≥ 24 小时
  ├─ 上次扫描后 ≥ 10 分钟（避免频繁触发）
  └─ 自上次整合后有 ≥ 5 个新会话
```

### 做什么？

```
Phase 1: 了解现状
  → 读取 MEMORY.md 索引，看看有哪些主题

Phase 2: 收集新信息
  → 翻阅最近几次会话的 transcript

Phase 3: 整合更新
  → 合并相关信息，修正过时内容

Phase 4: 剪枝 + 更新索引
  → 保持 MEMORY.md 在 200 行以内
```

---

## TeamMem — 团队共享记忆

多人协作时，团队成员可以共享项目记忆。

### 同步机制

```
本地写入 AutoMem
  │ 2 秒防抖
  ▼
Push 到服务端（只上传内容有变化的文件）
  │
  ▼
其他团队成员 Pull → 合并到本地
```

**注意**：服务端优先（server wins）——Pull 时服务端内容覆盖本地。本地删除文件不会同步到服务端（下次 Pull 会把它拉回来）。

### 安全防护

Push 前会扫描文件中是否包含 API key、token 等敏感信息，如果发现了，跳过该文件并报告用户。

---

## 各类记忆的比较

| | CLAUDE.md | AutoMem | Session Memory | TeamMem |
|--|-----------|---------|----------------|---------|
| **谁来写** | 你 | AI 自动 | AI 自动 | AI 自动 |
| **跨会话** | ✓ | ✓ | ✗ | ✓ |
| **跨用户** | 项目级可以 | ✗ | ✗ | ✓（团队）|
| **需要操作** | 手动编辑 | 无需 | 无需 | 无需 |
| **存储位置** | 项目/用户目录 | `~/.claude/projects/` | 同左 | 服务端 + 本地 |

---

## 设计哲学

**为什么所有记忆写入都是后台非阻塞的？**
因为记忆提取本身需要 AI 处理，如果同步执行会让用户等待。通过 forked agent 在后台运行，主对话完全不受影响。

**为什么 forked agent 权限受限（只能写 memory 目录）？**
安全考虑——记忆提取的 agent 没必要有读写任意文件的权限，最小化权限原则。

**为什么 AutoMem 路径使用 canonical git root 而不是 cwd？**
多个 git worktree 共享同一个代码库，它们应该共享同一份记忆，而不是各自维护一份。

---

## 关键文件

| 文件 | 里面有什么 |
|------|-----------|
| `src/utils/claudemd.ts` | CLAUDE.md 的发现、加载、解析 |
| `src/memdir/paths.ts` | AutoMem 路径解析逻辑 |
| `src/services/extractMemories/extractMemories.ts` | 自动提取记忆的触发和执行 |
| `src/services/extractMemories/memoryTypes.ts` | 四类记忆的类型定义 |
| `src/services/SessionMemory/sessionMemory.ts` | 会话笔记的管理 |
| `src/services/autoDream/autoDream.ts` | 定期记忆整合 |
| `src/services/teamMemorySync/index.ts` | 团队同步逻辑 |
| `src/skills/bundled/remember.ts` | `/remember` 技能（手动触发记忆审查） |
