package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/hankwenyx/claude-code-go/pkg/agent"
	"github.com/hankwenyx/claude-code-go/pkg/api"
	"github.com/hankwenyx/claude-code-go/pkg/permissions"
)

// RunOptions configures TUI launch behaviour.
type RunOptions struct {
	// SessionID is used for auto-saving the conversation to disk.
	// If empty, no auto-save occurs.
	SessionID string

	// ResumeHistory pre-populates the conversation with a prior session's messages.
	ResumeHistory []api.APIMessage

	// BriefMode enables chat-style view: tool calls are hidden and only
	// SendUserMessage output is rendered as the assistant reply.
	BriefMode bool
}

// Run launches the interactive TUI and blocks until the user quits.
// It wires up the AskFunc on checker so permission questions flow through
// the TUI instead of being auto-denied.
func Run(ctx context.Context, opts agent.AgentOptions, checker *permissions.Checker, runOpts ...RunOptions) error {
	var ro RunOptions
	if len(runOpts) > 0 {
		ro = runOpts[0]
	}

	askCh := make(chan permAskRequest)
	doneCh := make(chan struct{})

	// Wire AskFunc: agent goroutine sends question; TUI answers via resp channel.
	// doneCh prevents deadlock if the TUI exits while agent is waiting.
	checker.AskFunc = func(toolName, arg, reason string) bool {
		respCh := make(chan bool, 1)
		select {
		case askCh <- permAskRequest{toolName: toolName, arg: arg, reason: reason, resp: respCh}:
			select {
			case ans := <-respCh:
				return ans
			case <-doneCh:
				return false
			}
		case <-doneCh:
			return false
		}
	}

	// Glamour renderer (auto-detect dark/light; fall back gracefully)
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0),
	)

	// Build textarea with custom key bindings so plain Enter submits.
	// Call Focus() here (not only in Init) because Init() uses a value receiver —
	// mutations inside Init are discarded, so the textarea would start unfocused
	// and silently drop all keystrokes.
	ta := textarea.New()
	ta.KeyMap = newTextareaKeyMap()
	ta.Placeholder = "Ask Claude anything… (Enter to send, Ctrl+Enter for newline)"
	ta.ShowLineNumbers = false
	ta.SetHeight(2)
	ta.Focus() // must be called before Model construction

	// Build viewport (placeholder size, WindowSizeMsg will resize)
	vp := viewport.New(80, 20)

	m := Model{
		width:               80,
		height:              24,
		viewport:            vp,
		textarea:            ta,
		outputBuf:           new(strings.Builder),
		inputReady:          true,
		ctx:                 ctx,
		opts:                opts,
		permAskCh:           askCh,
		doneCh:              doneCh,
		renderer:            renderer,
		sessionID:           ro.SessionID,
		sessionCWD:          opts.CWD,
		conversationHistory: ro.ResumeHistory,
		briefMode:           ro.BriefMode,
	}

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()

	// Signal AskFunc goroutines that TUI has exited
	close(doneCh)
	return err
}
