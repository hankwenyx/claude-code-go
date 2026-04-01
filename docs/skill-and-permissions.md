# 技能（Skill）与权限系统

这篇文档介绍两个相关但独立的系统：
- **Skill 系统**：可复用的 AI 工作流，通过斜杠命令或 SkillTool 触发
- **权限系统**：控制 AI 能做什么、不能做什么

---

## 技能系统（Skill）

### 技能是什么？

技能就是**带元数据的 Markdown 文件**，描述了一个可复用的任务流程。比如：

```markdown
---
name: security-review
description: 对指定文件进行安全漏洞审查
allowed-tools:
  - Read
  - Grep
  - Bash(grep:*)
---

请对以下文件进行安全审查，重点检查：
1. SQL 注入漏洞
2. XSS 漏洞
3. 不安全的依赖

文件：$ARGUMENTS
```

用户可以通过 `/security-review src/auth.ts` 触发，也可以让模型通过 `SkillTool` 调用。

---

### 技能从哪里来？

技能按优先级从多个位置加载：

```
优先级（低 → 高）

/etc/claude-code/.claude/skills/   企业管理员设置的技能（强制）
~/.claude/skills/                  你的全局个人技能
ancestor/.claude/skills/           祖先目录的项目技能
.claude/skills/                    当前项目的技能（最高优先级）
  └── 同名文件，靠近 cwd 的版本获胜
```

每个技能是一个**目录**（不是单个文件）：

```
.claude/skills/
  security-review/
    SKILL.md      ← 必须叫这个名字（不区分大小写）
    helper.sh     ← 可选的辅助文件（用 $CLAUDE_SKILL_DIR 引用）
```

### 技能 frontmatter 全字段

```yaml
---
name: "显示名称"          # 可选，覆盖目录名
description: "一句话描述" # 供 SkillTool 搜索和模型判断
when_to_use: "何时调用"   # 提示模型什么时候应该主动调用这个技能

argument-hint: "<file> [--fix]"  # 用户调用时的参数提示
arguments:                        # 声明的参数名（用于 $arg_name 插值）
  - file

allowed-tools:             # 这个技能允许自动使用的工具
  - Read
  - Bash(git:*)

model: "opus"              # 可选覆盖模型（inherit 表示继承）
effort: "high"             # 努力等级

context: "fork"            # 执行上下文：省略=inline，fork=独立子 Agent
user-invocable: true       # 是否可以被用户通过 / 直接调用
disable-model-invocation: false  # 禁止模型主动调用

paths:                     # 条件激活（只在指定路径时激活）
  - "src/**/*.ts"
---
```

### 技能内容的特殊语法

```markdown
$ARGUMENTS           ← 用户传入的全部参数
$file                ← 单个参数（frontmatter 中声明的）
${CLAUDE_SKILL_DIR}  ← 这个技能所在目录的绝对路径
${CLAUDE_SESSION_ID} ← 当前会话 ID

!`git log --oneline -5`  ← 行内 shell 执行（本地技能，不在 MCP 中）
```

---

### 技能的两种执行方式

```
方式 A：Inline（默认）
  技能的 Markdown 内容 → 注入为用户消息 → 模型在当前对话继续执行
  优点：上下文完整，可以访问整个对话历史
  适合：大多数场景

方式 B：Fork（context: fork）
  技能内容 → 启动独立子 Agent → 在隔离上下文执行 → 返回结果文本
  优点：不污染主对话，有独立的 token budget
  适合：需要长时间执行、或结果需要汇总的场景
```

---

### 内置技能清单

| 技能名 | 功能 | 执行方式 |
|--------|------|---------|
| `simplify` | 审查代码变更的复用性/质量，并行启动 3 个子 Agent | inline |
| `batch` | 大规模并行变更（5-30 个 worktree Agent 同时工作） | inline |
| `remember` | 审查自动记忆，提议晋升到 CLAUDE.md | inline |
| `debug` | 读取调试日志，诊断当前会话问题 | inline |
| `skillify` | 把当前会话的工作流固化为可复用技能 | inline |
| `update-config` | 修改 settings.json（权限/hooks/环境变量） | inline |
| `lorem-ipsum` | 生成 Lorem Ipsum 占位文本 | inline |
| `loop` | 循环执行（定时触发）[AGENT_TRIGGERS] | inline |
| `claude-api` | Claude API 使用示例（多语言版本） | inline |
| `keybindings` | 键盘绑定配置帮助 | inline |

---

### 条件激活技能

技能可以只在特定文件类型下激活（`paths` 字段）：

```
.claude/skills/react-component-review/
  SKILL.md 中：
    paths:
      - "src/**/*.tsx"
      - "src/**/*.jsx"
```

这个技能平时不加载，只有当你操作 `.tsx` / `.jsx` 文件时才动态激活，避免工具列表无谓膨胀。

---

### 技能 vs 命令 vs 工具

| | 技能（Skill） | 命令（Command） | 工具（Tool） |
|--|-------------|----------------|------------|
| **谁能调用** | 用户 + 模型 | 仅用户（`/`前缀） | 仅模型 |
| **定义方式** | Markdown 文件 | TypeScript 代码 | TypeScript 代码 |
| **可扩展** | 用户自定义 | 内置，不可自定义 | 内置 + MCP |
| **本质** | prompt 注入 | 本地执行或 prompt | API 调用 |

---

## 权限系统

### 为什么需要权限系统？

AI 可以执行 `rm -rf /`，也可以提交代码到 main 分支。权限系统确保 AI 的每个高危操作都经过你的确认（或符合你预设的规则）。

### 四种权限模式

用 `Shift+Tab` 在模式间切换：

```
Default（默认）
  → 每个工具调用都弹确认框
  → 你可以"本次允许"或"永久允许（加规则）"

Accept Edits（接受编辑）
  → 工作目录内的文件读写自动允许
  → Bash 等高危操作仍需确认
  → 适合：你信任模型的代码修改，但不想放开 shell

Plan Mode（计划模式）
  → 只允许读操作（Read / Grep / Glob）
  → 所有写操作需要你审批
  → 适合：先让 AI 分析，你审核计划后再执行

Bypass Permissions（跳过权限）
  → 跳过所有权限检查
  → 有严格限制：需要在 sandbox 环境 + 无网络（ant 内部要求）
  → 慎用！
```

---

### 权限规则是怎么定义的？

规则存在 `settings.json` 的 `permissions` 字段：

```json
{
  "permissions": {
    "allow": [
      "Read",                  // 允许所有 Read 调用
      "Bash(git:*)",           // 允许所有 git 命令
      "Edit(.claude/**)",      // 允许编辑 .claude 目录下的文件
      "Skill(commit)"          // 允许调用 commit 技能
    ],
    "deny": [
      "Bash(rm -rf:*)"         // 禁止 rm -rf
    ],
    "ask": [
      "Bash(npm publish:*)"    // npm publish 需要每次确认
    ]
  }
}
```

规则存储在多个 settings.json 中，优先级从低到高：

```
/etc/claude-code/...settings.json  企业策略（最低，但有最高覆盖权）
~/.claude/settings.json            用户全局
.claude/settings.json              项目级
.claude/settings.local.json        本地覆盖（最高优先级）
```

---

### 权限检查的完整流程

```
工具请求执行
       │
       ▼
① 有 deny 规则匹配？ → 直接拒绝，返回错误给模型
       │
       ▼
② 是 safe 路径（.git / .vscode / .bashrc 等危险文件）？
   → 无论什么模式都提示确认
       │
       ▼
③ 当前是什么权限模式？
   ├─ bypass → 直接允许（跳到⑦）
   ├─ plan   → 写操作需要审批
   └─ default / acceptEdits → 继续
       │
       ▼
④ 有 allow 规则匹配？ → 直接允许（跳到⑦）
       │
       ▼
⑤ 有 ask 规则匹配？ → 弹确认框
       │
       ▼
⑥ 默认行为：弹确认框
       │
       ▼
⑦ 执行工具
```

---

### Shell 规则的三种匹配方式

`Bash(...)` 括号内支持三种语法：

```
精确匹配：  git status        → 只匹配完全一样的命令
前缀匹配：  git:*             → 匹配所有 git 开头的命令（遗留冒号语法）
通配符：    git * --no-pager  → * 作为通配符（类似 glob）
            npm install *     → 匹配所有 npm install xxx
```

特殊规则：`git *` 中尾部的 `space + *` 是可选的，所以 `git *` 同时匹配 `git add`（有参数）和裸 `git`（无参数）。

---

### 文件路径权限

文件操作有独立的路径权限系统：

```
路径语法：
  //path        → 绝对路径 /path
  /path         → 相对于 settings.json 所在目录
  ~/path        → home 目录展开
  src/**/*.ts   → glob 模式

检查顺序：
  1. deny 规则？→ 拒绝
  2. 是危险文件（.gitconfig / .bashrc / .vscode/ 等）？→ 强制提示
  3. 在工作目录内？→ read 允许；write 需要 acceptEdits 模式
  4. 在 sandbox allowlist 内？→ 允许
  5. 有 allow 规则匹配？→ 允许
  6. 默认拒绝
```

---

### 企业管理员如何限制用户？

企业级 `policySettings` 有最高覆盖权，可以：

```
allowManagedPermissionRulesOnly: true
  → 禁止用户添加自定义权限规则
  → 界面上隐藏"永久允许"按钮

isRestrictedToPluginOnly('skills')
  → 只允许使用插件提供的技能，禁止用户自定义技能

sandbox.network.allowManagedDomainsOnly
  → WebFetch 只能访问白名单域名

shouldDisableBypassPermissions()（GrowthBook）
  → 动态禁用 bypass 模式
```

---

### --dangerously-skip-permissions 的安全门

这个参数等于开启 `bypassPermissions` 模式，但有额外保护：

```
运行前检查：
  ├─ Unix 系统 root 用户（uid=0）？→ 直接退出，除非在 bubblewrap/sandbox
  └─ ant 内部用户必须满足：
       ├─ 运行在 Docker / bubblewrap / IS_SANDBOX=1 容器内
       └─ 无法访问互联网（hasInternet === false）
       → 违反条件直接 process.exit(1)

即使开启 bypass，这些情况仍然强制提示：
  ├─ 工具自身返回 deny
  ├─ 危险文件路径（.git / .vscode / .bashrc 等）
  └─ 工具要求必须与用户交互（requiresUserInteraction=true）
```

---

### 自动权限模式（Auto/Yolo）

`auto` 模式用 AI 分类器代替用户做权限决策（仅 ant 内部可用）：

```
工具请求执行
  │
  ▼
调用 ML 分类器（两阶段）：
  Stage 1：快速粗判断
  Stage 2：不确定时精细判断
  │
  ├─ 安全 → 自动允许
  ├─ 危险 → 弹确认框
  └─ 分类器不可用 → 失败关闭（拒绝执行）
```

---

### 沙箱（Sandbox）

沙箱模式让 Bash 命令在受限环境中运行：

```
SandboxManager
  ├─ 文件系统限制：只能读/写 allowWrite 列表中的路径
  ├─ 网络限制：只能访问 allowedDomains 列表中的域名
  └─ 违规行为记录：SandboxViolationStore

例外设置（settings.json 中配置）：
  sandbox:
    excludedCommands:       # 豁免沙箱的命令
      - "npm install"
    filesystem:
      allowWrite:           # 额外允许写入的路径
        - "/tmp/my-app"
```

---

## 关键文件

| 文件 | 里面有什么 |
|------|-----------|
| `src/skills/loadSkillsDir.ts` | 技能发现、加载、条件激活 |
| `src/tools/SkillTool/SkillTool.ts` | SkillTool 完整实现（inline/fork 路由） |
| `src/skills/bundled/index.ts` | 内置技能注册 |
| `src/utils/permissions/permissions.ts` | 权限检查主引擎 |
| `src/utils/permissions/permissionsLoader.ts` | 规则从磁盘加载/保存 |
| `src/utils/permissions/shellRuleMatching.ts` | Shell 规则匹配（精确/前缀/通配符） |
| `src/utils/permissions/filesystem.ts` | 文件路径权限检查 |
| `src/utils/permissions/pathValidation.ts` | 路径安全验证（防 TOCTOU 等） |
| `src/utils/permissions/getNextPermissionMode.ts` | Shift+Tab 切换模式逻辑 |
| `src/utils/sandbox/sandbox-adapter.ts` | 沙箱适配层 |
| `src/setup.ts` | --dangerously-skip-permissions 安全门控 |
