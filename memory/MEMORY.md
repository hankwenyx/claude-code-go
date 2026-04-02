# Go Claude Code 项目记忆

## 项目位置
- 工作目录：`/Users/wenyuxuan/go/src/claude-code-go`
- Go module：`github.com/hankwenyx/claude-code-go`（原 wenyuxuan，已改名）
- 开发语言：Go 1.24

## 当前进度
- Phase 1a ✅ API Client + 最简 agent loop
- Phase 1b ✅ 配置加载（settings.json + CLAUDE.md）
- Phase 1c ✅ 工具系统（Bash/FileRead/FileEdit/FileWrite/Glob/Grep）+ 权限系统
- Phase 1d ✅ WebFetch + 工具结果截断 + 并发执行策略（只读并发/写操作串行）
- Phase 1e ✅ SDK 迁移 + 磁盘持久化 + credentials.json fallback + example_test.go
- **Phase 1 全部完成 ✅**

## 关键文件
- `pkg/api/client.go` — anthropic-sdk-go v1.29.0 封装，Beta.Messages.NewStreaming
- `pkg/api/types.go` — 内部类型 + StreamResponse 累积器
- `pkg/api/systemprompt.go` — System prompt 构建（2块：静态缓存+动态）
- `pkg/agent/agent.go` — 多轮工具调用循环（needsFollowUp 标志）
- `pkg/agent/options.go` — AgentOptions 结构
- `pkg/config/settings.go` — settings.json 4层加载合并
- `pkg/config/claudemd.go` — CLAUDE.md 加载（@include、AutoMem、HTML注释剥除）
- `pkg/config/permissions.go` — 权限规则匹配（MatchRule）
- `pkg/permissions/checker.go` — 权限检查引擎（Checker）
- `pkg/tools/` — 所有工具实现
- `cmd/claude/main.go` — CLI 入口

## 关键设计决策
- SDK Client 封装：anthropic.Client 是值类型（非指针），`inner anthropic.Client`
- SDK 流式：`Beta.Messages.NewStreaming()` 返回 `*ssestream.Stream[BetaRawMessageStreamEventUnion]`
- 联合类型字段名：`OfTool`/`OfEnabled`/`OfToolUse`/`OfToolResult`/`OfText`/`OfThinking`/`OfRedactedThinking`（去掉了旧的 `OfBeta*` 前缀）
- Delta 类型：`BetaTextDelta`/`BetaInputJSONDelta`/`BetaThinkingDelta`（非 `BetaRaw*`）
- CLAUDE.md 作为合成 user 消息注入（`<system-reminder>` 包裹），不注入 system prompt
- needsFollowUp 不依赖 stop_reason，在 streaming 期间检测 content_block_start{type:"tool_use"} 设置
- 所有 tool_result 必须在同一条 user 消息中（API 不允许连续两条 user 消息）
- Bash 权限规则用自定义 globMatch（* 可匹配 /），文件工具用 filepath.Match

## 用户偏好
- 要写开发日志（DEVLOG.md），记录所有代码的单测和功能测试
- 不确定实现细节时，参照 origin_src（TypeScript 源码）
- 从 plan.md 拆解 roadmap 到 README，实现过程中更新进度
- module 名为 github.com/hankwenyx/claude-code-go
