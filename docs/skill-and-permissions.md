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

### 技能从哪里来？（加载顺序与去重）

`getSkillDirCommands(cwd)` 函数负责技能发现，结果经 `memoize` 缓存。加载用 `Promise.all` 并行执行，**先加载的优先级更高**，同名技能取最先出现的版本（first-wins）：

```
加载顺序（优先级从高到低）

1. managedSkills    getManagedFilePath()/.claude/skills    企业管理员强制技能
                    （可通过 CLAUDE_CODE_DISABLE_POLICY_SKILLS 环境变量禁用）
2. userSkills       getClaudeConfigHomeDir()/skills        用户全局技能
3. projectSkills    getProjectDirsUpToHome('skills', cwd)  项目技能（向上遍历到 home）
4. additionalSkills --add-dir 参数指定目录/.claude/skills   显式添加的目录
5. legacyCommands   /commands/ 目录                        旧版命令（已废弃）

--bare 模式：跳过 1-3，只加载 4（--add-dir 显式指定的路径）
```

**去重机制**：通过 `realpath()` 解析符号链接，获取文件的规范路径作为唯一标识（`getFileIdentity()`），预先并行计算所有路径的 identity，用 `Map<string, source>` 进行 first-wins 去重。

每个技能是一个**目录**（不是单个文件）：

```
.claude/skills/
  security-review/
    SKILL.md      ← 必须叫这个名字（不区分大小写）
    helper.sh     ← 可选的辅助文件（用 $CLAUDE_SKILL_DIR 引用）
```

---

### 动态技能发现（`discoverSkillDirsForPaths`）

当你编辑文件时，系统会动态扫描文件路径是否能激活新的技能目录：

```
你编辑 src/components/Button.tsx
    │
    ▼
从 src/components/ 向上遍历到 cwd（不含 cwd 本身）
    │
    ├─ 检查每层目录下的 .claude/skills/
    ├─ 跳过 gitignored 的目录（isPathGitignored()）
    └─ 发现的技能目录按深度排序（深层优先）
           │
           ▼
    通过 addSkillDirectories() 加载，存入 dynamicSkills Map
    已检查过的目录记入 Set，避免重复 stat 调用
```

---

### 技能 frontmatter 全字段

```yaml
---
name: "显示名称"          # 可选，覆盖目录名（不影响唯一 ID）
description: "一句话描述" # 供 SkillTool 搜索和模型判断
when_to_use: "何时调用"   # 提示模型什么时候应该主动调用这个技能（注意下划线）

argument-hint: "<file> [--fix]"  # 用户调用时的参数提示（UI 显示）
arguments:                        # 声明的参数名（用于 $arg_name 插值）
  - file

allowed-tools:             # 这个技能允许自动使用的工具
  - Read
  - Bash(git:*)

model: "opus"              # 可选覆盖模型（inherit 表示继承父模型）
effort: "high"             # 努力等级（影响 thinking budget）

context: "fork"            # 执行上下文：省略=inline，fork=独立子 Agent
user-invocable: true       # 是否可以被用户通过 / 直接调用（默认 true）
disable-model-invocation: false  # 禁止模型主动调用（仅允许用户触发）

paths:                     # 条件激活（只在指定路径匹配时加载）
  - "src/**/*.ts"

hooks:                     # 技能生命周期钩子（HooksSchema 验证）
  ...
agent: "..."               # 指定执行 agent 类型
version: "1.0"             # 版本号
---
```

**`description` 字段**的处理：优先使用 `coerceDescriptionToString()`，如果 frontmatter 没有 description，则 fallback 到 `extractDescriptionFromMarkdown()`（从文档首段提取）。

---

### 技能内容的特殊语法

```markdown
$ARGUMENTS           ← 用户传入的全部参数（原始字符串）
$file                ← 单个参数（frontmatter 中 arguments 声明的）
${CLAUDE_SKILL_DIR}  ← 这个技能所在目录的绝对路径（Windows 上 \ 转为 /）
${CLAUDE_SESSION_ID} ← 当前会话 ID

!`git log --oneline -5`  ← 行内 shell 执行（本地技能，MCP 中不支持）
```

---

### 技能的两种执行方式

```
方式 A：Inline（默认，未设置 context 或 context 不是 'fork'）
  ┌────────────────────────────────────────────┐
  │ processPromptSlashCommand() 处理技能        │
  │ → 技能内容注入为新的 user messages          │
  │ → 返回 newMessages + contextModifier        │
  │   （contextModifier 更新 alwaysAllowRules、│
  │    模型、effort 等）                        │
  │ → 模型在同一对话上下文中继续执行            │
  └────────────────────────────────────────────┘
  优点：上下文完整，可访问完整对话历史
  适合：大多数场景

方式 B：Fork（context: fork）
  ┌────────────────────────────────────────────┐
  │ executeForkedSkill()                        │
  │ → prepareForkedCommandContext() 创建隔离上下文│
  │ → runAgent() 在独立 token budget 内执行     │
  │ → extractResultText() 提取结果文本          │
  │ → 返回 { status: 'forked', agentId, result }│
  │ → 子 Agent 消息收集后立即释放               │
  │   （agentMessages.length = 0）             │
  └────────────────────────────────────────────┘
  优点：不污染主对话，有独立的 token budget
  适合：需要长时间执行、或结果需要汇总的场景
```

> **实验性功能（仅 ant 内部）**：`EXPERIMENTAL_SKILL_SEARCH` feature flag 开启时，支持远程规范技能（`_canonical_<slug>` 前缀），从 AKI/GCS 加载 SKILL.md（带本地缓存），直接注入为 user message。

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

### 条件激活技能（`paths` 字段详解）

技能可以只在特定文件类型下激活：

```yaml
paths:
  - "src/**/*.tsx"
  - "src/**/*.jsx"
```

**解析阶段（`parseSkillPaths()`）**：
- 将字符串/数组拆分为 pattern 列表
- 自动去除 `/**` 后缀（`ignore` 库自动处理目录内匹配）
- 若所有 pattern 均为 `**`（全匹配）→ 视为无限制（返回 `undefined`）

**存储阶段**：有 `paths` 的技能不直接加入可用技能列表，存入 `conditionalSkills Map` 等待激活。

**激活阶段（`activateConditionalSkillsForPaths()`）**：

```
你对某个文件进行操作
    │
    ▼
用 ignore 库（gitignore 语义）匹配 paths patterns
    │
    ├─ 相对路径（非 ../、非绝对路径）才能被匹配
    │
    └─ 任意一个文件匹配 → 立刻激活技能
         → 从 conditionalSkills 移入 dynamicSkills
         → 触发 skillsLoaded.emit()（通知缓存清除）
         → break（一个文件匹配即可）
```

这让技能列表保持精简——只有当你实际操作相关文件时，技能才出现在模型视野中。

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

### 权限检查的完整流程（`hasPermissionsToUseToolInner`）

```
工具请求执行
       │
       ▼
── 规则检查阶段（在 bypass 判断之前，优先处理规则和安全检查）──
       │
① deny 规则匹配整个 tool？
   → 直接 deny（最高优先级）
       │
② ask 规则匹配？
   → 检查是否可沙箱自动允许（仅 Bash + 沙箱启用 + autoAllowBashIfSandboxed）
   → 否则 ask
       │
③ tool.checkPermissions() → tool 实现自己的判断
   │
   ├─ deny → 直接 deny
   ├─ requiresUserInteraction() + ask → 直接 ask（不受 bypass 影响）
   └─ ask + decisionReason.type==='safetyCheck' → 直接 ask（安全检查免疫 bypass）
       │
── 模式检查阶段 ──
       │
④ bypassPermissions 模式？→ allow
       │
⑤ 整个 tool 有 allow 规则（toolAlwaysAllowedRule）？→ allow
       │
⑥ passthrough → 转为 ask
       │
── 后处理（hasPermissionsToUseTool 中）──
       │
⑦ dontAsk 模式（headless/async）：ask → deny
       │
⑧ auto/yolo 模式（TRANSCRIPT_CLASSIFIER feature）：
   ├─ acceptEdits 快速路径：重新以 acceptEdits 模式检查，若通过直接 allow
   ├─ isAutoModeAllowlistedTool() allowlist → allow
   └─ 调用 ML 分类器 classifyYoloAction()
       ├─ 安全 → allow，重置连续拒绝计数
       └─ 危险 → deny，记录 denialTracking
           超过 DENIAL_LIMITS.maxTotal/maxConsecutive：
           ├─ headless → AbortError（停止整个 Agent）
           └─ 有 UI → fallback 到手动审批（重置计数）
       │
⑨ 执行工具
```

---

### Shell 规则的三种匹配方式（`parsePermissionRule` + `matchWildcardPattern`）

`Bash(...)` 括号内支持三种语法（`src/utils/permissions/shellRuleMatching.ts`）：

```
精确匹配（exact）：  git status
  → 正则 /^git status$/ 全匹配
  → 只匹配完全一样的命令字符串

前缀匹配（prefix）： git:*
  → 检测 /:*$/ 后缀（legacy 语法）
  → 提取 prefix 为 "git"
  → 匹配所有以 "git" 开头的命令

通配符匹配（wildcard）：git * --no-pager
  → 检测除 :* 之外的未转义 *（逐字符扫描前置反斜杠数量）
```

**通配符转换算法**（`matchWildcardPattern`）：
```
1. \* 替换为 NULL_BYTE 占位符 A
2. \\ 替换为 NULL_BYTE 占位符 B
3. 其余内容 escape 正则特殊字符
4. 未转义的 * 转为 .*（正则通配符）
5. 还原占位符
6. 特殊规则：pattern 以 ' *' 结尾且只有一个通配符时
   → 将末尾 ' .*' 改为 '( .*)?'（参数变为可选）
   → 这样 'git *' 既匹配 'git add' 也匹配裸 'git'
7. 用 /^pattern$/s 全匹配（s flag 使 . 匹配换行）
```

---

### 文件路径权限（`filesystem.ts` 详解）

**读权限（`checkReadPermissionForTool()`）检查顺序**：

```
① UNC 路径（\\ 或 // 开头）→ ask
② 可疑 Windows 路径（NTFS ADS、8.3 短名等）→ ask
③ Read deny 规则（对所有路径变体检查）→ deny（最高优先级）
④ Read ask 规则 → ask
⑤ Edit 权限 allow → Read 也 allow（复用写权限的判断）
⑥ 在 working directory 内 → allow
⑦ 内部可读路径（session-memory、plans、tool-results 等）→ allow
⑧ Read allow 规则 → allow
⑨ 默认：ask（带 suggestions）
```

**写权限（`checkWritePermissionForTool()`）检查顺序**：

```
① Edit deny 规则 → deny
② 内部可编辑路径（plan 文件、scratchpad、agent memory）→ allow
   （必须在安全检查之前，允许 AI 自我管理）
③ .claude/** 的 session 级 allow 规则 → allow
④ 综合安全检查（checkPathSafetyForAutoEdit()）：
   ├─ 可疑 Windows 路径 → ask（classifierApprovable: false）
   ├─ Claude 配置文件（settings.json、commands/、skills/）→ ask（可分类）
   └─ 危险文件（.bashrc、.gitconfig、.git/、.vscode/、.idea/等）→ ask（可分类）
⑤ Edit ask 规则 → ask
⑥ acceptEdits 模式 + 在 working directory 内 → allow
⑦ Edit allow 规则 → allow
⑧ 默认：ask（带 suggestions）
```

**路径语法**（`matchingRuleForInput()` 的解析方式）：
```
//path        → 绝对路径 /path
/path         → 相对于 settings.json 所在目录（CC 特有约定）
~/path        → home 目录展开
src/**/*.ts   → glob 模式（使用 ignore 库，gitignore 语义）
```

---

### 企业管理员如何限制用户？

企业级 `policySettings` 有最高覆盖权：

```
allowManagedPermissionRulesOnly: true
  → loadAllPermissionRulesFromDisk() 仅返回 policySettings 的规则
  → addPermissionRulesToSettings() 拒绝用户添加新规则
  → UI 中隐藏"永久允许"按钮（shouldShowAlwaysAllowOptions()）

isRestrictedToPluginOnly('skills')
  → 只允许使用插件提供的技能，禁止用户自定义技能

sandbox.network.allowManagedDomainsOnly: true
  → convertToSandboxRuntimeConfig() 中只使用 policySettings 的域名
  → sandbox ask 回调：shouldAllowManagedSandboxDomainsOnly() 为 true 时直接 false
  → 阻断所有新域名请求，无法通过 UI 授权

areSandboxSettingsLockedByPolicy()
  → 检查 policySettings 是否有 sandbox.enabled 等设置
  → 有则锁定，UI 中相应控件禁用（用户无法修改）

shouldDisableBypassPermissions()（GrowthBook）
  → 动态禁用 bypass 模式
```

---

### --dangerously-skip-permissions 的安全门

这个参数等于开启 `bypassPermissions` 模式，但有额外保护：

```
运行前检查：
  ├─ Unix root 用户（uid=0）？→ 直接退出，除非在 bubblewrap/sandbox
  └─ ant 内部用户必须满足：
       ├─ 运行在 Docker / bubblewrap / IS_SANDBOX=1 容器内
       └─ 无法访问互联网（hasInternet === false）
       → 违反条件直接 process.exit(1)

即使开启 bypass，这些情况仍然强制提示：
  ├─ 工具自身返回 deny（requiresUserInteraction=true）
  ├─ 危险文件路径（.git / .vscode / .bashrc 等）
  └─ 规则明确 ask 的操作（decisionReason.type === 'rule'）
```

---

### 自动权限模式（Auto/Yolo）

`auto` 模式用 AI 分类器代替用户做权限决策（受 `TRANSCRIPT_CLASSIFIER` feature flag 控制）：

```
工具请求执行
  │
  ├─ acceptEdits 快速路径：重新以 acceptEdits 模式调用 checkPermissions()
  │   → 通过则直接 allow（跳过 API 调用）
  │
  ├─ isAutoModeAllowlistedTool() → 直接 allow
  │
  └─ 调用 ML 分类器：
       formatActionForClassifier(tool.name, input)
         → setClassifierChecking(toolUseID)  // UI: "正在检查..."
         → classifyYoloAction(messages, action, tools, ...)
         → clearClassifierChecking(toolUseID)

结果处理：
  shouldBlock=false → allow，重置连续拒绝计数
  shouldBlock=true：
    ├─ transcriptTooLong → fallback 到手动审批
    ├─ unavailable：tengu_iron_gate_closed ?
    │    ├─ true  → fail-closed（deny）
    │    └─ false → fallback 到手动审批
    └─ 正常拒绝：记录 denialTracking
         超过 DENIAL_LIMITS.maxTotal/maxConsecutive：
         ├─ headless → AbortError
         └─ 有 UI   → 强制 fallback 到手动审批（重置计数）
```

---

### 沙箱（Sandbox）详解

沙箱底层是 `@anthropic-ai/sandbox-runtime` 的 adapter（`SandboxManager`），在受限环境中运行 Bash 命令。

**`isSandboxingEnabled()` 四重检查**：
```
① BaseSandboxManager.isSupportedPlatform()
   → 支持：macOS / Linux / WSL2（不支持 WSL1）
② checkDependencies().errors.length === 0（检查 bwrap 等依赖，memoized）
③ isPlatformInEnabledList()（检查 enabledPlatforms 配置）
④ getSandboxEnabledSetting()（用户设置 sandbox.enabled === true）
```

**配置构建（`convertToSandboxRuntimeConfig()`）**：

```
网络配置（allowedDomains）：
  从 WebFetch(domain:xxx) 格式的权限规则中提取域名
  → allowManagedDomainsOnly 时只用 policySettings 的域名

文件系统配置：
  allowWrite 默认包含：
    ├─ 当前目录 "."
    └─ getClaudeTempDir()（临时文件目录）

  自动 denyWrite（重要安全保护）：
    ├─ 所有 settings 文件路径
    ├─ managed settings drop-in 目录
    ├─ .claude/skills/ 目录（防止技能被写入篡改）
    └─ bare git repo 文件（HEAD/objects/refs/hooks/config）
         → 防止 core.fsmonitor 攻击

  从权限规则中提取：
    ├─ FILE_EDIT_TOOL 的 allow 规则路径 → 加入 allowWrite
    └─ FILE_EDIT/READ_TOOL 的 deny 规则路径 → 加入 denyWrite/denyRead

  路径语法差异（重要）：
    ├─ 权限规则中的 /path → settings 文件目录下的相对路径（CC 约定）
    └─ sandbox.filesystem.* 中的 /path → 绝对路径（标准语义）
```

**动态配置更新**：`initialize()` 后订阅 `settingsChangeDetector`，settings 变化时自动 `BaseSandboxManager.updateConfig()` 刷新。

**git worktree 支持**：`initialize()` 时调用 `detectWorktreeMainRepoPath()`，读取 `.git` 文件内容（`gitdir: /path/to/main/repo/.git/worktrees/name`）检测 worktree，主仓库路径自动加入 `allowWrite`（git 操作需要写主仓库的 `.git` 目录）。

**`cleanupAfterCommand()`**：除标准清理外，额外调用 `scrubBareGitRepoFiles()` 删除命令期间可能被植入的 bare git repo 文件，防止 `core.fsmonitor` 攻击。

```
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
| `src/skills/loadSkillsDir.ts` | 技能发现、加载、条件激活、动态发现 |
| `src/tools/SkillTool/SkillTool.ts` | SkillTool 完整实现（inline/fork/远程规范路由） |
| `src/skills/bundled/index.ts` | 内置技能注册 |
| `src/utils/permissions/permissions.ts` | 权限检查主引擎（`hasPermissionsToUseToolInner`） |
| `src/utils/permissions/permissionsLoader.ts` | 规则从磁盘加载/保存，企业策略实现 |
| `src/utils/permissions/shellRuleMatching.ts` | Shell 规则匹配（精确/前缀/通配符，通配符转正则） |
| `src/utils/permissions/filesystem.ts` | 文件路径权限检查（读/写完整流程） |
| `src/utils/permissions/pathValidation.ts` | 路径安全验证（防 TOCTOU、UNC 路径等） |
| `src/utils/permissions/getNextPermissionMode.ts` | Shift+Tab 切换模式逻辑 |
| `src/utils/sandbox/sandbox-adapter.ts` | 沙箱适配层（`convertToSandboxRuntimeConfig`） |
| `src/setup.ts` | --dangerously-skip-permissions 安全门控 |
