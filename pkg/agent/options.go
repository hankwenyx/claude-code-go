package agent

import (
	"github.com/hankwenyx/claude-code-go/pkg/api"
	"github.com/hankwenyx/claude-code-go/pkg/hooks"
	"github.com/hankwenyx/claude-code-go/pkg/permissions"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
	"github.com/hankwenyx/claude-code-go/pkg/tools/task"
)

// AgentOptions configures the agent
type AgentOptions struct {
	// APIKey is the Anthropic API key
	APIKey string

	// Model is the model to use (default: claude-sonnet-4-6)
	Model string

	// MaxTokens is the maximum output tokens (default: 4096)
	MaxTokens int

	// MaxTurns is the maximum number of turns (0 = 50 default)
	MaxTurns int

	// SystemPrompt is the system prompt blocks
	SystemPrompt []api.SystemBlock

	// ClaudeMdContent is the formatted CLAUDE.md content (injected as synthetic user message)
	ClaudeMdContent string

	// Registry holds the registered tools
	Registry *tools.Registry

	// PermChecker handles permission checking for tool calls
	PermChecker *permissions.Checker

	// NonInteractive: ask permissions automatically become deny (headless mode)
	NonInteractive bool

	// APIBaseURL overrides the API base URL
	APIBaseURL string

	// Client is a custom API client (optional, overrides APIKey/Model/MaxTokens)
	Client *api.Client

	// CWD is the working directory, used for tool result persistence paths.
	// If empty, os.Getwd() is used when needed.
	CWD string

	// SessionID identifies this agent session for tool result persistence.
	// If empty, a random ID is generated on first use.
	SessionID string

	// Messages is the prior conversation history to resume from.
	// If non-nil, the agent appends to this slice rather than starting fresh.
	// The caller may pass the updated slice (returned via EventMessage) on the
	// next turn to maintain multi-turn context.
	Messages []api.APIMessage

	// TaskManager tracks background sub-agent tasks (Phase 4b/4c).
	// When set, the agent loop drains completed-task notifications before each
	// API turn and injects <task-notification> messages so the model is aware.
	TaskManager *task.Manager

	// HookRunner executes pre/post tool-use hooks (Phase 6).
	// When set, RunPreToolUse is called before each tool execution and
	// RunPostToolUse is called after. A non-empty block reason from
	// RunPreToolUse prevents the tool from running.
	HookRunner *hooks.Runner

	// CompactThreshold is the total token count (input+output) that triggers
	// automatic conversation compaction. 0 disables automatic compaction.
	// When triggered, the agent summarises prior messages and continues.
	CompactThreshold int
}
