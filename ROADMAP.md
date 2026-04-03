# Roadmap / 路线图

[English](#english) | [中文](#中文)

---

## English

### Phase 1 — Headless Core ✅

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 1a | SSE streaming API client + minimal agent loop | ✅ Done |
| 1b | settings.json 4-layer loading + CLAUDE.md with @include | ✅ Done |
| 1c | Tool interface + Bash / FileRead / FileEdit / FileWrite / Glob / Grep + permissions | ✅ Done |
| 1d | WebFetch + result truncation + parallel read / serial write | ✅ Done |
| 1e | SDK migration + disk session persistence + credentials.json fallback | ✅ Done |

### Phase 2 — TUI ✅

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 2a | REPL with Markdown rendering | ✅ Done |
| 2b | Tool call UI: spinner, name, args, result | ✅ Done |
| 2c | Permission dialog: y/n/always/skip | ✅ Done |
| 2d | Multi-line input, history navigation, `@file` mention | ✅ Done |
| 2e | Slash commands: `/clear` `/compact` `/model` `/help` `/exit` | ✅ Done |
| 2f | Status bar: token usage + model name | ✅ Done |
| 2g | Brief/chat mode (`--brief`) | ✅ Done |
| 2h | Type-ahead queue: compose while agent runs, Esc to cancel | ✅ Done |

### Phase 3 — MCP ✅

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 3a | MCP stdio transport: subprocess + JSON-RPC | ✅ Done |
| 3b | `tools/list` → dynamic tool registration | ✅ Done |
| 3c | `tools/call` → execution + result | ✅ Done |
| 3d | SSE transport (HTTP MCP server) | ✅ Done |
| 3e | settings.json `mcpServers` auto-connect | ✅ Done |
| 3f | MCP permission rules (`mcp__server__tool` format) | ✅ Done |

### Phase 4 — Multi-Agent ✅ (partial)

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 4a | Sync sub-agent: blocking `Task` tool | ✅ Done |
| 4b | Async sub-agent with `task_id` | ✅ Done |
| 4c | Task notifications injected into main agent | ✅ Done |
| 4d | 30s panel auto-eviction after completion | 🔲 Todo |
| 4e | TodoWrite / TodoRead (in-memory) | ✅ Done |
| 4f | TodoV2: disk-persisted, dependency graph, multi-agent safe | 🔲 Todo |
| 4g | Worktree isolation | 🔲 Todo |

### Phase 5 — Memory ✅ (partial)

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 5a | Session compaction at token threshold | ✅ Done |
| 5b | AutoMem: extract + persist memories after compact | ✅ Done |
| 5c | AutoDream: periodic consolidation with PID lock | 🔲 Todo |
| 5d | MEMORY.md: 200-line / 25 KB cap, atomic write | ✅ Done |

### Phase 6 — Hooks ✅ (partial)

| Sub-phase | Content | Status |
|-----------|---------|--------|
| 6a | PreToolUse: block or modify input | ✅ Done |
| 6b | PostToolUse: modify output | ✅ Done |
| 6c | Notification: fire-and-forget | ✅ Done |
| 6d | HTTP hooks (`allowedHttpHookUrls`) | 🔲 Todo |

### Phase 7 — Advanced (planned)

| Feature | Priority |
|---------|----------|
| Prompt cache optimization | High |
| Extended Thinking (`ultrathink`) | Medium |
| Skills (slash command extensions) | Medium |
| StatusLine plugin | Medium |
| Sandbox (bubblewrap, Linux only) | Low |
| Web Browser tool (Playwright) | Low |

---

## 中文

### Phase 1 — Headless 核心 ✅

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 1a | SSE 流式 API 客户端 + 最简 agent loop | ✅ 完成 |
| 1b | settings.json 四层加载 + CLAUDE.md（含 @include） | ✅ 完成 |
| 1c | Tool 接口 + Bash / FileRead / FileEdit / FileWrite / Glob / Grep + 权限系统 | ✅ 完成 |
| 1d | WebFetch + 工具结果截断 + 只读并行/写串行 | ✅ 完成 |
| 1e | SDK 迁移 + 磁盘会话持久化 + credentials.json 回退 | ✅ 完成 |

### Phase 2 — TUI ✅

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 2a | REPL + Markdown 渲染 | ✅ 完成 |
| 2b | 工具调用 UI：spinner、名称、参数、结果 | ✅ 完成 |
| 2c | 权限对话框：y/n/always/skip | ✅ 完成 |
| 2d | 多行输入、历史导航、`@file` 提及 | ✅ 完成 |
| 2e | Slash 命令：`/clear` `/compact` `/model` `/help` `/exit` | ✅ 完成 |
| 2f | 状态栏：token 用量 + 模型名 | ✅ 完成 |
| 2g | 简洁/聊天模式（`--brief`） | ✅ 完成 |
| 2h | 消息排队：Agent 运行时可输入，Esc 撤回 | ✅ 完成 |

### Phase 3 — MCP ✅

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 3a | MCP stdio 传输：子进程 + JSON-RPC | ✅ 完成 |
| 3b | `tools/list` → 动态工具注册 | ✅ 完成 |
| 3c | `tools/call` → 执行 + 返回结果 | ✅ 完成 |
| 3d | SSE 传输（HTTP MCP 服务器） | ✅ 完成 |
| 3e | settings.json `mcpServers` 自动连接 | ✅ 完成 |
| 3f | MCP 权限规则（`mcp__server__tool` 格式） | ✅ 完成 |

### Phase 4 — 多 Agent ✅（部分完成）

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 4a | 同步子 Agent：阻塞式 `Task` 工具 | ✅ 完成 |
| 4b | 异步子 Agent，返回 `task_id` | ✅ 完成 |
| 4c | 任务通知注入主 Agent | ✅ 完成 |
| 4d | 完成后 30s 面板自动销毁 | 🔲 待做 |
| 4e | TodoWrite / TodoRead（内存版） | ✅ 完成 |
| 4f | TodoV2：磁盘持久化、依赖关系、多 Agent 安全 | 🔲 待做 |
| 4g | Worktree 隔离 | 🔲 待做 |

### Phase 5 — 记忆系统 ✅（部分完成）

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 5a | 达到 token 阈值时压缩对话 | ✅ 完成 |
| 5b | AutoMem：compact 后提取并持久化记忆 | ✅ 完成 |
| 5c | AutoDream：定期记忆整合（PID 锁 + 回滚） | 🔲 待做 |
| 5d | MEMORY.md：200 行 / 25 KB 截断，原子写入 | ✅ 完成 |

### Phase 6 — Hooks ✅（部分完成）

| 子阶段 | 内容 | 状态 |
|--------|------|------|
| 6a | PreToolUse：阻断或修改工具输入 | ✅ 完成 |
| 6b | PostToolUse：修改工具输出 | ✅ 完成 |
| 6c | Notification：fire-and-forget | ✅ 完成 |
| 6d | HTTP hooks（`allowedHttpHookUrls`） | 🔲 待做 |

### Phase 7 — 高级功能（规划中）

| 功能 | 优先级 |
|------|--------|
| Prompt cache 优化 | 高 |
| Extended Thinking（`ultrathink`） | 中 |
| Skills（slash 命令扩展） | 中 |
| StatusLine 插件 | 中 |
| 沙箱（bubblewrap，仅 Linux） | 低 |
| Web Browser 工具（Playwright） | 低 |
