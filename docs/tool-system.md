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

## 工具的完整接口

每个工具都实现同一套接口（`src/Tool.ts`）：

```
必须实现：
  name              工具名（模型调用时用）
  inputSchema       参数格式定义（Zod）
  call()            执行逻辑

可选实现：
  validateInput()   参数语义验证（Zod 之后的额外检查）
  checkPermissions()权限决策（allow / deny / ask 用户）
  isReadOnly()      是否只读（影响并发执行策略）
  isConcurrencySafe()是否可与其他工具并发

UI 相关：
  renderToolUseMessage()    工具被调用时的 UI（streaming 期间实时渲染）
  renderToolResultMessage() 工具完成后的 UI
  getActivityDescription()  spinner 文字（如 "Reading src/foo.ts"）
  userFacingName()          左侧工具标签显示名
```

---

## 工具的完整执行流程

```
模型调用工具
      │
      ▼
① Zod schema 验证输入参数
      │ 失败 → 返回 InputValidationError 给模型（不终止循环）
      ▼
② tool.validateInput()（语义检查，如"文件未读就不能编辑"）
      │ 失败 → 返回错误给模型
      ▼
③ 执行 PreToolUse Hooks（用户自定义钩子，可修改输入/阻止执行）
      │
      ▼
④ 权限检查 canUseTool()
      │ deny  → 返回"用户拒绝"给模型
      │ ask   → 弹出权限对话框，等待用户确认
      │ allow → 继续
      ▼
⑤ tool.call()（实际执行）
      │ 异常 → 捕获，包装为 is_error tool_result 返回给模型
      ▼
⑥ 执行 PostToolUse Hooks
      │
      ▼
⑦ 结果返回给模型（作为下一轮的 user 消息）
```

**关键设计**：工具执行失败不会终止 Agent Loop，错误作为工具结果返回给模型，让模型自己决定怎么处理。

---

## 并发执行策略

不是所有工具都能同时执行，规则是这样的：

```
只读工具（isReadOnly=true）：
  Read / Grep / Glob / WebFetch
  → 可以互相并发执行，最多同时 10 个

写操作工具（isReadOnly=false）：
  Bash / FileEdit / FileWrite
  → 必须串行，等前一个完成才执行下一个

特殊情况：
  Bash 执行出错 → 自动取消同批次的其他工具
  （因为后续 Bash 命令可能依赖前面的结果）
```

```
示例：模型一次调用了 5 个工具
  Read("a.ts")  ← 只读 ┐
  Read("b.ts")  ← 只读 ├─ 三个并发执行
  Grep("foo")   ← 只读 ┘
  Edit("a.ts")  ← 写操作 → 等上面三个完成后执行
  Bash("test")  ← 写操作 → 等 Edit 完成后执行
```

---

## 核心工具详解

### Bash — 执行 Shell 命令

最强大也最危险的工具，做了很多安全处理：

**命令分类**（影响 UI 折叠显示）

```
搜索命令  → find / grep / rg / ag          显示为可折叠的搜索结果
读取命令  → cat / head / tail / wc          显示为可折叠的读取结果
列表命令  → ls / tree / du                  显示为可折叠的列表结果
静默命令  → mv / cp / rm / mkdir            成功时显示 "Done"（无输出）
其他命令  → 完整显示输出
```

**长时间命令的处理**

```
执行时间 > 2s    → 开始显示实时进度（避免用户以为卡死了）
用户按 Ctrl+B    → 转为后台运行（可用 TaskOutput 查看输出）
run_in_background=true → 模型主动要求后台运行
执行时间 > 2min  → 自动后台化（助手模式下）
```

**大输出处理**

```
输出 > 30,000 字符
  │
  ├─ 持久化到磁盘文件
  └─ 返回给模型：
     <persisted-output>
     输出太大（X bytes），已保存到：/path/to/file
     预览（前 2KB）：
     [内容前 2000 字节...]
     </persisted-output>
```

**sed 命令特殊处理**

当模型发出 `sed -i 's/old/new/' file` 这样的命令时，Claude Code 会：
1. 解析 sed 语法（BRE → JS 正则转换）
2. 在权限对话框里**展示实际的文件变更预览**（不是原始命令）
3. 用户确认后，直接用解析结果写入（跳过 shell 执行），确保"你看到的就是写进去的"

**沙箱模式**

开启沙箱后，Bash 命令在受限环境中执行（限制文件系统访问和网络）。部分命令可以配置豁免沙箱。

---

### FileRead — 读取文件

支持多种文件类型，不只是纯文本：

```
.py / .ts / .go 等  → 带行号的文本内容
.png / .jpg 等      → base64 图片（发给模型视觉能力）
.pdf               → 有 pages 参数 → 提取指定页转 JPEG
                     无 pages 参数 → 原生 PDF 内容
.ipynb             → Jupyter notebook（cells 数组格式）
```

**智能去重**：如果同一个文件读过一次且没有修改，再次 Read 时返回"文件没有变化"而不是重新发送内容（节省 token）。

**Token 限制**：先粗估（字符数换算），超过阈值的 1/4 才精确计数。超出上限时提示用模型用 offset/limit 分段读取。

**安全检查**：阻止读取 `/dev/zero`、`/dev/random` 等无限输出设备；读取文本文件后追加安全提醒（防止文件内容注入恶意指令）。

---

### FileEdit — 精确编辑文件

这是最精密的工具，有严格的"先读后写"保护：

```
模型调用 Edit(file, old_string, new_string)
        │
① 检查这个文件是否读取过（未读就编辑 → 报错）
        │
② 检查文件的修改时间（读取后文件被改过 → 报错，防止覆盖他人修改）
        │
③ 在文件中找到 old_string
   ├─ 找不到 → 尝试"引号归一化"再找（处理智能引号/直引号混用）
   ├─ 找到多处 + replace_all=false → 报错（避免意外替换）
   └─ 找到一处 → 继续
        │
④ 原子写入（同步操作，不 await，防止并发写入）
        │
⑤ 通知 LSP（TypeScript 等语言服务器更新诊断）
⑥ 通知 VSCode（更新 diff 视图）
⑦ 更新文件状态缓存（记录新的修改时间）
```

返回给模型的是简洁的确认消息（"文件已更新"），不是 diff 内容。UI 里展示 diff 供用户查看。

---

### Grep — 内容搜索

封装了 ripgrep，有三种输出模式：

```
files_with_matches（默认）
  → 返回包含匹配的文件路径列表
  → 按修改时间排序（最近修改的排前面）
  → 默认最多 250 条

content
  → 展示匹配行的内容（支持 -A/-B/-C 上下文行）
  → 支持 -n 显示行号，-i 不区分大小写，-U 多行匹配

count
  → 每个文件的匹配数量统计
```

自动排除 `.git`、`.svn` 等版本控制目录，限制每行最长 500 字符（避免 base64/minified 内容污染搜索结果）。

---

### WebFetch — 抓取网页内容

不是简单地返回 HTML，而是进行了"理解"处理：

```
请求 URL
  │
  ├─ 检查域名是否在白名单 → 自动允许
  ├─ 不在白名单 → 弹权限对话框
  │
  ▼
获取 HTML，转换为 Markdown
  │
  ├─ 跨域重定向？→ 返回特殊响应，让模型重新调用新 URL
  │              （不自动跟随，避免授权了 A 域名结果跳到 B 域名）
  │
  ├─ 内容较小 → 直接返回 Markdown 文本
  └─ 内容很大 → 用 Haiku 模型对内容回答你的 prompt，只返回相关部分
                （"用小模型帮你摘要"）
```

---

### MCPTool — MCP 服务器代理

MCP（Model Context Protocol）工具是动态生成的——运行时根据连接的 MCP 服务器来创建。

```
MCP 服务器连接成功
  │
  ▼
拉取服务器的工具列表
  │
  ▼
为每个工具生成一个 MCPTool 实例：
  name:     mcp__<serverName>__<toolName>
  schema:   直接使用服务器提供的 JSON Schema
  call():   透传调用服务器，处理认证/重试/大输出截断
  权限:     passthrough → 交由通用权限系统处理
```

MCP 工具的 `checkPermissions()` 返回 `passthrough`，意味着"让通用权限规则来决定"。用户可以配置 `mcp__server__tool` 格式的允许/拒绝规则。

---

### AgentTool — 启动子 Agent

这是 Claude Code 的"元工具"——用工具来启动另一个 AI Agent。

```
AgentTool 调用
  │
  ├─ 同步模式（等待结果）→ runAgent() → 独立查询循环 → 返回最终结果
  │
  ├─ 异步模式（后台运行）→ registerAsyncAgent() → 立刻返回 task_id
  │                        后续用 TaskOutput 查看输出
  │
  ├─ worktree 隔离 →  创建独立 git worktree → 在里面运行
  │                   Agent 完成后检查变更，可选清理
  │
  └─ 远程执行 →  在 Anthropic 云端 CCR 环境运行
                 本地轮询远端状态
```

子 Agent 有自己独立的：消息历史、AbortController、读写文件状态缓存。
但共享父 Agent 的：工具列表（受限版本）、MCP 连接、Prompt Cache 前缀。

**权限继承规则**：父 Agent 已批准的权限**不会自动**传给子 Agent，子 Agent 只拥有显式声明的工具白名单。这防止了权限升级攻击。

---

## 工具结果太大怎么处理？

每个工具都有 `maxResultSizeChars` 限制：

| 工具 | 限制 | 超出后 |
|------|------|--------|
| BashTool | 30,000 字符 | 持久化到磁盘，模型收到文件引用 |
| FileEditTool / WebFetchTool | 100,000 字符 | 同上 |
| GrepTool | 20,000 字符 | 同上 |
| FileReadTool | 无限制 | 自身有 token 机制，不走持久化 |

持久化的结果：
```
<persisted-output>
Output too large (X bytes). Full output saved to: /path/to/file
Preview (first 2KB):
[内容前 2000 字节...]
...（截断）
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
```

---

## 关键文件

| 文件 | 里面有什么 |
|------|-----------|
| `src/Tool.ts` | 工具接口定义、ToolUseContext、buildTool 工厂函数 |
| `src/tools.ts` | 所有工具的注册和组装逻辑 |
| `src/tools/BashTool/` | Bash 工具（命令分类、沙箱、后台任务、sed 解析） |
| `src/tools/FileEditTool/` | 文件编辑（冲突检测、原子写入） |
| `src/tools/FileReadTool/` | 文件读取（多格式、去重、token 限制） |
| `src/tools/WebFetchTool/` | 网页抓取（Haiku 摘要、重定向检测） |
| `src/tools/MCPTool/` | MCP 代理工具模板 |
| `src/services/mcp/client.ts` | MCP 工具动态生成（第 1743 行） |
| `src/tools/AgentTool/` | 子 Agent 调度 |
| `src/services/tools/StreamingToolExecutor.ts` | 并发调度核心 |
| `src/services/tools/toolExecution.ts` | 工具执行链（权限→执行→钩子） |
| `src/utils/toolResultStorage.ts` | 大结果持久化 |
