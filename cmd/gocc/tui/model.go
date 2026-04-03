package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/hankwenyx/claude-code-go/pkg/agent"
	"github.com/hankwenyx/claude-code-go/pkg/api"
	"github.com/hankwenyx/claude-code-go/pkg/memory"
	"github.com/hankwenyx/claude-code-go/pkg/session"
)

// toolRow tracks the display state of a single tool invocation
type toolRow struct {
	id      string
	name    string
	summary string
	spinner spinner.Model
	done    bool
	isError bool
	errMsg  string
	hidden  bool // true in brief mode for non-SendUserMessage tools
}

// permAskRequest carries a permission question from the agent goroutine to the TUI
type permAskRequest struct {
	toolName string
	arg      string
	reason   string
	resp     chan<- bool
}

// turnRecord holds a completed conversation turn for display
type turnRecord struct {
	userMsg  string
	rendered string // glamour-rendered assistant reply
	toolRows []toolRow
}

// errQuit is a sentinel used by /exit and /quit slash commands to signal TUI exit.
var errQuit = errors.New("quit")

// private message types for bubbletea Update dispatch
type agentEventMsg agent.AgentEvent
type agentDoneMsg struct{}
type permAskMsg permAskRequest

// Model is the top-level bubbletea model for the TUI
type Model struct {
	width, height int
	viewport      viewport.Model
	textarea      textarea.Model
	inputReady    bool

	// current turn state
	currentInput string           // user message submitted this turn
	outputBuf    *strings.Builder // raw streaming text (before full message)
	rendered     string           // glamour-rendered text after EventMessage
	toolRows     []toolRow        // tools invoked this turn

	// agent pipeline
	ctx       context.Context
	cancel    context.CancelFunc
	eventChan <-chan agent.AgentEvent
	opts      agent.AgentOptions // saved for re-use per turn

	// token usage (cumulative across all turns)
	totalInputTokens  int
	totalOutputTokens int

	// session persistence
	sessionID  string
	sessionCWD string

	// multi-turn conversation history (passed back to RunAgent each turn)
	conversationHistory []api.APIMessage

	// permission flow
	permAskCh  <-chan permAskRequest
	pendingAsk *permAskRequest
	doneCh     chan struct{} // closed on TUI exit so AskFunc unblocks

	// session allow rules added via 'a' key
	sessionAllowRules []string

	// input history (most recent last); historyIdx is the current browse position
	// len(inputHistory) means "at the bottom" (current draft)
	inputHistory []string
	historyIdx   int
	historyDraft string // saved draft when browsing up

	// completed turns
	history []turnRecord

	// renderer
	renderer *glamour.TermRenderer

	// briefMode: when true (--brief / settings.json defaultView:chat),
	// tool calls are collapsed and only SendUserMessage output is shown.
	briefMode bool

	// retryStatus: shown in status bar during retry wait ("⟳ rate limited, retry 1/3 in 4s…")
	retryStatus string

	// pendingInputs: messages queued while agent is running (auto-submitted in order when agent finishes)
	pendingInputs []string

	fatalErr error
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.textarea.Focus(),
		listenPermAsk(m.permAskCh),
	)
}

// listenAgent returns a Cmd that reads the next event from the agent channel
func listenAgent(ch <-chan agent.AgentEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return agentDoneMsg{}
		}
		return agentEventMsg(ev)
	}
}

// listenPermAsk returns a Cmd that reads the next permission request
func listenPermAsk(ch <-chan permAskRequest) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-ch
		if !ok {
			return nil
		}
		return permAskMsg(req)
	}
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle /exit sentinel
	if m.fatalErr == errQuit {
		return m, tea.Quit
	}

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		viewH := m.height - 4 // viewport takes all but status+sep+textarea
		if viewH < 3 {
			viewH = 3
		}
		m.viewport.Width = m.width
		m.viewport.Height = viewH
		m.textarea.SetWidth(m.width)
		m = m.rebuildViewport()

	case tea.KeyMsg:
		// Ctrl+C always quits
		if key.Matches(msg, keys.Quit) {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}

		// When a permission ask is pending, only handle perm keys
		if m.pendingAsk != nil {
			return m.handlePermKey(msg)
		}

		// Scroll keys work regardless of agent state
		if key.Matches(msg, keys.ScrollUp) {
			m.viewport.ScrollUp(5)
			return m, nil
		}
		if key.Matches(msg, keys.ScrollDown) {
			m.viewport.ScrollDown(5)
			return m, nil
		}

		if m.eventChan == nil {
			// Agent idle — normal submit/history/textarea handling
			if key.Matches(msg, keys.Submit) {
				input := strings.TrimSpace(m.textarea.Value())
				if input != "" {
					// Handle slash commands before dispatching to agent
					if handled, newModel, slashCmd := m.handleSlashCommand(input); handled {
						// /exit returns tea.Quit directly
						if slashCmd != nil {
							return newModel, slashCmd
						}
						// /compact sets an eventChan — kick off the listener
						if newModel.eventChan != nil {
							return newModel, listenAgent(newModel.eventChan)
						}
						return newModel, nil
					}
					return m.submitInput(input)
				}
				return m, nil
			}
			// History navigation — only when textarea is on a single line
			if key.Matches(msg, keys.HistoryPrev) && m.textarea.Line() == 0 {
				return m.historyNavigate(-1), nil
			}
			if key.Matches(msg, keys.HistoryNext) && m.textarea.Line() == 0 {
				return m.historyNavigate(+1), nil
			}
			// Pass to textarea
			var taCmd tea.Cmd
			m.textarea, taCmd = m.textarea.Update(msg)
			cmds = append(cmds, taCmd)
		} else {
			// Agent running — allow composing the next message; queue on Enter
			if key.Matches(msg, keys.Submit) {
				input := strings.TrimSpace(m.textarea.Value())
				if input != "" {
					m.pendingInputs = append(m.pendingInputs, input)
					m.textarea.Reset()
					m = m.rebuildViewport()
				}
				return m, tea.Batch(cmds...)
			}
			// Escape cancels the last queued message
			if key.Matches(msg, keys.CancelQueue) && len(m.pendingInputs) > 0 {
				m.pendingInputs = m.pendingInputs[:len(m.pendingInputs)-1]
				m = m.rebuildViewport()
				return m, tea.Batch(cmds...)
			}
			// Pass keystrokes to textarea so user can type the next message
			var taCmd tea.Cmd
			m.textarea, taCmd = m.textarea.Update(msg)
			cmds = append(cmds, taCmd)
		}

	// Drive spinners
	case spinner.TickMsg:
		for i, row := range m.toolRows {
			if !row.done {
				var cmd tea.Cmd
				m.toolRows[i].spinner, cmd = row.spinner.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
		m = m.rebuildViewport()

	case agentEventMsg:
		m, cmds2 := m.handleAgentEvent(agent.AgentEvent(msg))
		return m, tea.Batch(append(cmds, cmds2...)...)

	case agentDoneMsg:
		// Turn complete — archive to history
		m.history = append(m.history, turnRecord{
			userMsg:  m.currentInput,
			rendered: m.rendered,
			toolRows: m.toolRows,
		})
		m.currentInput = ""
		m.rendered = ""
		m.outputBuf = new(strings.Builder)
		m.toolRows = nil
		m.eventChan = nil
		m = m.rebuildViewport()
		// re-enable input
		m.inputReady = true
		cmds = append(cmds, m.textarea.Focus())
		// Auto-submit the first queued message (typed while agent was running)
		if len(m.pendingInputs) > 0 {
			pending := m.pendingInputs[0]
			m.pendingInputs = m.pendingInputs[1:]
			newModel, submitCmd := m.submitInput(pending)
			return newModel, tea.Batch(append(cmds, submitCmd)...)
		}

	case permAskMsg:
		m.pendingAsk = &permAskRequest{
			toolName: msg.toolName,
			arg:      msg.arg,
			reason:   msg.reason,
			resp:     msg.resp,
		}
		m = m.rebuildViewport()
		// keep listening for more
		cmds = append(cmds, listenPermAsk(m.permAskCh))

	}

	// Propagate to viewport/textarea if no special handling
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

// handleAgentEvent processes a single agent event
func (m Model) handleAgentEvent(ev agent.AgentEvent) (Model, []tea.Cmd) {
	var cmds []tea.Cmd
	cmds = append(cmds, listenAgent(m.eventChan))

	switch ev.Type {
	case agent.EventText:
		m.retryStatus = "" // clear any lingering retry banner once response starts
		m.outputBuf.WriteString(ev.Text)
		m = m.rebuildViewport()

	case agent.EventMessage:
		// Full message received — render with glamour
		raw := m.outputBuf.String()
		if m.renderer != nil {
			if out, err := m.renderer.Render(raw); err == nil {
				m.rendered = out
			} else {
				m.rendered = raw
			}
		} else {
			m.rendered = raw
		}
		// Accumulate token usage
		if ev.Usage != nil {
			m.totalInputTokens += ev.Usage.InputTokens
			m.totalOutputTokens += ev.Usage.OutputTokens
		}
		// Save updated conversation history for next turn
		if len(ev.Messages) > 0 {
			m.conversationHistory = ev.Messages
		}
		// Auto-save session to disk (best-effort, ignore errors)
		if m.sessionID != "" {
			_ = session.Save(session.Record{
				ID:        m.sessionID,
				CWD:       m.sessionCWD,
				Model:     m.opts.Model,
				CreatedAt: time.Now(), // overwritten on update by Save
				Messages:  m.conversationHistory,
			})
		}
		m = m.rebuildViewport()

	case agent.EventToolUse:
		m.retryStatus = "" // clear retry banner when model starts acting
		sp := spinner.New()
		sp.Spinner = spinner.Dot
		sp.Style = toolSpinnerStyle
		row := toolRow{
			id:      ev.ToolCall.ID,
			name:    ev.ToolCall.Name,
			summary: toolSummaryFromInput(ev.ToolCall.Name, ev.ToolCall.Input),
			spinner: sp,
			hidden:  m.briefMode && ev.ToolCall.Name != "SendUserMessage",
		}
		m.toolRows = append(m.toolRows, row)
		m = m.rebuildViewport()
		cmds = append(cmds, sp.Tick)

	case agent.EventToolResult:
		for i, row := range m.toolRows {
			if row.id == ev.ToolResult.ToolUseID {
				m.toolRows[i].done = true
				m.toolRows[i].isError = ev.ToolResult.IsError
				if ev.ToolResult.IsError {
					m.toolRows[i].errMsg = ev.ToolResult.Content
				}
				// In brief mode, render SendUserMessage result as assistant reply
				if m.briefMode && row.name == "SendUserMessage" && !ev.ToolResult.IsError {
					raw := ev.ToolResult.Content
					if m.renderer != nil {
						if out, err := m.renderer.Render(raw); err == nil {
							m.rendered = out
						} else {
							m.rendered = raw
						}
					} else {
						m.rendered = raw
					}
				}
				break
			}
		}
		m = m.rebuildViewport()

	case agent.EventRetry:
		secs := int(ev.RetryIn.Seconds() + 0.5)
		short := ev.RetryErr.Error()
		if len(short) > 60 {
			short = short[:60] + "…"
		}
		m.retryStatus = fmt.Sprintf("⟳ 请求受限，%ds 后重试（第 %d 次）…  %s", secs, ev.Attempt, short)
		m = m.rebuildViewport()

	case agent.EventError:
		// Non-fatal rendering: show error inline, restore input so user can retry
		m.retryStatus = ""
		errMsg := ev.Error.Error()
		if m.renderer != nil {
			if out, err := m.renderer.Render("**错误：** " + errMsg); err == nil {
				m.rendered = out
			} else {
				m.rendered = "错误：" + errMsg + "\n"
			}
		} else {
			m.rendered = "错误：" + errMsg + "\n"
		}
		// Archive as a turn so the error appears in history, then reset for next input
		m.history = append(m.history, turnRecord{
			userMsg:  m.currentInput,
			rendered: m.rendered,
			toolRows: m.toolRows,
		})
		m.currentInput = ""
		m.rendered = ""
		m.outputBuf = new(strings.Builder)
		m.toolRows = nil
		m.eventChan = nil
		m.inputReady = true
		m = m.rebuildViewport()
		cmds = append(cmds, m.textarea.Focus())
	}

	return m, cmds
}

// handlePermKey handles keyboard input when a permission dialog is shown
func (m Model) handlePermKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pendingAsk == nil {
		return m, nil
	}

	switch {
	case key.Matches(msg, keys.PermYes):
		m.pendingAsk.resp <- true
		m.pendingAsk = nil
	case key.Matches(msg, keys.PermNo), key.Matches(msg, keys.PermSkip):
		m.pendingAsk.resp <- false
		m.pendingAsk = nil
	case key.Matches(msg, keys.PermAlways):
		// Add session allow rule and approve
		rule := m.pendingAsk.toolName
		if m.pendingAsk.arg != "" {
			rule += "(" + m.pendingAsk.arg + ")"
		}
		m.sessionAllowRules = append(m.sessionAllowRules, rule)
		m.pendingAsk.resp <- true
		m.pendingAsk = nil
	default:
		return m, nil
	}

	m = m.rebuildViewport()
	// Resume listening for the next permission request
	return m, listenPermAsk(m.permAskCh)
}

// submitInput starts a new agent turn
func (m Model) submitInput(input string) (Model, tea.Cmd) {
	// Save to history (skip if same as last entry or if it's a slash command)
	if !strings.HasPrefix(input, "/") {
		if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != input {
			m.inputHistory = append(m.inputHistory, input)
		}
	}
	// Reset history browse position to end
	m.historyIdx = len(m.inputHistory)
	m.historyDraft = ""

	m.inputReady = false
	m.currentInput = input
	m.textarea.Reset()
	m.outputBuf = new(strings.Builder)
	m.rendered = ""
	m.toolRows = nil

	// Expand @file mentions in the user's input before sending to agent
	expandedInput := expandAtMentions(input, m.opts.CWD)

	// Build per-turn options without mutating the shared opts.
	opts := m.opts
	// Copy Allow slice to avoid accumulating rules across turns.
	basAllow := opts.PermChecker.Rules.Allow
	merged := make([]string, len(basAllow)+len(m.sessionAllowRules))
	copy(merged, basAllow)
	copy(merged[len(basAllow):], m.sessionAllowRules)
	opts.PermChecker.Rules.Allow = merged

	// Resume from prior conversation history
	opts.Messages = m.conversationHistory

	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	ch := agent.RunAgent(ctx, expandedInput, opts)
	m.eventChan = ch

	m = m.rebuildViewport()
	return m, listenAgent(m.eventChan)
}

// newTextareaKeyMap builds a custom KeyMap for the textarea so that plain
// Enter submits the message while Ctrl+Enter inserts a newline.
func newTextareaKeyMap() textarea.KeyMap {
	km := textarea.DefaultKeyMap
	// Disable enter-as-newline so the outer Update can intercept it.
	km.InsertNewline = key.NewBinding(
		key.WithKeys("ctrl+enter", "ctrl+j"),
		key.WithHelp("ctrl+enter", "insert newline"),
	)
	return km
}

// handleSlashCommand intercepts /clear, /help, /model, and /compact typed by the user.
// Returns (true, newModel) when the command was handled, (false, m) otherwise.
func (m Model) handleSlashCommand(input string) (bool, Model, tea.Cmd) {
	m.textarea.Reset()
	cmd := strings.ToLower(strings.TrimSpace(input))

	switch {
	case cmd == "/clear":
		m.history = nil
		m.conversationHistory = nil
		m.currentInput = ""
		m.outputBuf = new(strings.Builder)
		m.rendered = ""
		m.toolRows = nil
		m.totalInputTokens = 0
		m.totalOutputTokens = 0
		m.inputHistory = nil
		m.historyIdx = 0
		m.historyDraft = ""
		m = m.rebuildViewport()
		return true, m, nil

	case cmd == "/help":
		helpText := `**Keyboard shortcuts**
| Key | Action |
|-----|--------|
| Enter | Send message |
| Ctrl+Enter | Insert newline |
| ↑ / Ctrl+P | Previous input (history) |
| ↓ / Ctrl+N | Next input (history) |
| PgUp / PgDn | Scroll |
| Ctrl+C | Quit |

**Slash commands**
| Command | Description |
|---------|-------------|
| /clear | Clear conversation history and token counter |
| /compact | Summarise conversation to reduce token usage |
| /model [name] | Show or switch the current model |
| /help | Show this help |

**@file mentions**
| Syntax | Description |
|--------|-------------|
| @path/to/file | Inline file contents into your message |
| @./relative/path | Relative path from working directory |

**Permission dialog**
| Key | Action |
|-----|--------|
| y | Allow once |
| n | Deny |
| a | Always allow (session) |
| s | Skip (deny) |
`
		if m.renderer != nil {
			if out, err := m.renderer.Render(helpText); err == nil {
				m.rendered = out
			} else {
				m.rendered = helpText
			}
		} else {
			m.rendered = helpText
		}
		m = m.rebuildViewport()
		return true, m, nil

	case cmd == "/model" || strings.HasPrefix(cmd, "/model "):
		parts := strings.SplitN(strings.TrimSpace(input), " ", 2)
		if len(parts) == 1 || strings.TrimSpace(parts[1]) == "" {
			current := m.opts.Model
			if current == "" {
				current = "claude-sonnet-4-6 (default)"
			}
			notice := "Current model: **" + current + "**\n\nTo switch: `/model claude-opus-4-6`"
			if m.renderer != nil {
				if out, err := m.renderer.Render(notice); err == nil {
					m.rendered = out
				} else {
					m.rendered = notice
				}
			} else {
				m.rendered = notice
			}
		} else {
			newModel := strings.TrimSpace(parts[1])
			m.opts.Model = newModel
			m.opts.Client = api.NewClient(m.opts.APIKey,
				api.WithModel(newModel),
				api.WithBaseURL(m.opts.APIBaseURL),
			)
			notice := "Switched to model: **" + newModel + "**"
			if m.renderer != nil {
				if out, err := m.renderer.Render(notice); err == nil {
					m.rendered = out
				} else {
					m.rendered = notice
				}
			} else {
				m.rendered = notice
			}
		}
		m = m.rebuildViewport()
		return true, m, nil

	case cmd == "/compact":
		return true, m.startCompact(), nil

	case cmd == "/exit" || cmd == "/quit":
		if m.cancel != nil {
			m.cancel()
		}
		return true, m, tea.Quit
	}

	return false, m, nil
}

// startCompact uses memory.CompactMessages to summarise the conversation
// and replaces the history with the compact summary.
func (m Model) startCompact() Model {
	if len(m.conversationHistory) == 0 {
		m.rendered = "_No conversation to compact._\n"
		m = m.rebuildViewport()
		return m
	}

	m.inputReady = false
	m.currentInput = "/compact"
	m.outputBuf = new(strings.Builder)
	m.rendered = ""
	m.toolRows = nil

	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel

	// Run compaction in a goroutine and stream a synthetic event sequence
	wrapped := make(chan agent.AgentEvent, 10)
	history := m.conversationHistory
	client := m.opts.Client
	cwd := m.opts.CWD

	go func() {
		defer close(wrapped)
		compacted, summaryText, err := memory.CompactMessages(ctx, client, history)
		if err != nil {
			wrapped <- agent.AgentEvent{Type: agent.EventError, Error: err}
			return
		}
		// Emit compact event so the UI can show feedback
		wrapped <- agent.AgentEvent{Type: agent.EventCompact}
		// Emit a synthetic message event carrying the compacted history
		wrapped <- agent.AgentEvent{
			Type:     agent.EventMessage,
			Messages: compacted,
			Message: &api.Message{
				Content: []api.ContentBlock{{Type: "text", Text: summaryText}},
			},
		}

		// Opportunistically save memories after compaction (Phase 5b)
		if cwd != "" && client != nil {
			go func() {
				memCtx, memCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer memCancel()
				if bullets, merr := memory.ExtractMemories(memCtx, client, summaryText); merr == nil && bullets != "" {
					memory.AppendMemory(cwd, bullets) //nolint:errcheck
				}
			}()
		}
	}()

	m.eventChan = wrapped
	m = m.rebuildViewport()
	return m
}

// historyNavigate moves through input history.
// delta=-1 goes to older entry, delta=+1 goes to newer entry.
func (m Model) historyNavigate(delta int) Model {
	if len(m.inputHistory) == 0 {
		return m
	}

	// Save current draft before first upward move
	if delta == -1 && m.historyIdx == len(m.inputHistory) {
		m.historyDraft = m.textarea.Value()
	}

	newIdx := m.historyIdx + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx > len(m.inputHistory) {
		newIdx = len(m.inputHistory)
	}
	m.historyIdx = newIdx

	var text string
	if newIdx == len(m.inputHistory) {
		text = m.historyDraft // restore draft
	} else {
		text = m.inputHistory[newIdx]
	}

	m.textarea.SetValue(text)
	return m
}

// expandAtMentions replaces @path tokens in the input with the file contents.
// Tokens that are not readable files are left as-is with a warning appended.
func expandAtMentions(input, cwd string) string {
	words := strings.Fields(input)
	if len(words) == 0 {
		return input
	}

	var hasAt bool
	for _, w := range words {
		if strings.HasPrefix(w, "@") {
			hasAt = true
			break
		}
	}
	if !hasAt {
		return input
	}

	// Replace @path tokens with file content blocks
	result := input
	for _, w := range words {
		if !strings.HasPrefix(w, "@") {
			continue
		}
		relPath := w[1:]
		if relPath == "" {
			continue
		}
		absPath := relPath
		if !strings.HasPrefix(relPath, "/") {
			absPath = cwd + "/" + relPath
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			// Leave token, append error note
			result = strings.Replace(result, w, w+"[file not found]", 1)
			continue
		}
		block := "<file_content path=\"" + relPath + "\">\n" + string(data) + "\n</file_content>"
		result = strings.Replace(result, w, block, 1)
	}
	return result
}
