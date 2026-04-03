package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View implements tea.Model
func (m Model) View() string {
	if m.fatalErr != nil && m.fatalErr != errQuit {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n", m.fatalErr))
	}

	sep := separatorStyle.Render(strings.Repeat("─", m.width))

	// Right-side info: model name + cumulative tokens
	modelName := m.opts.Model
	if modelName == "" {
		modelName = "claude-sonnet-4-6"
	}
	tokenInfo := fmt.Sprintf("%s  ↑%d ↓%d tok", modelName, m.totalInputTokens, m.totalOutputTokens)
	rightInfo := statusStyle.Render(tokenInfo)

	// Status / permission line (left side)
	var leftStatus string
	if m.pendingAsk != nil {
		label := m.pendingAsk.toolName
		if m.pendingAsk.arg != "" {
			label += "(" + truncate80(m.pendingAsk.arg) + ")"
		}
		leftStatus = permStyle.Render("Allow "+label+"?") + " " +
			permKeyStyle.Render("[y]es") + " / " +
			permKeyStyle.Render("[n]o") + " / " +
			permKeyStyle.Render("[a]lways") + " / " +
			permKeyStyle.Render("[s]kip")
	} else if m.retryStatus != "" {
		leftStatus = retryStyle.Render(m.retryStatus)
	} else if m.eventChan != nil {
		if len(m.pendingInputs) > 0 {
			preview := m.pendingInputs[len(m.pendingInputs)-1]
			if len(preview) > 35 {
				preview = preview[:35] + "…"
			}
			suffix := ""
			if len(m.pendingInputs) > 1 {
				suffix = fmt.Sprintf(" (+%d)", len(m.pendingInputs)-1)
			}
			leftStatus = statusStyle.Render("Running…  ·  ⏎ 已排队: " + preview + suffix + "  Esc撤回")
		} else {
			leftStatus = statusStyle.Render("Running…  (可输入下一条消息，Enter 排队)")
		}
	} else if m.inputReady {
		leftStatus = statusStyle.Render("Enter to send · /help · /clear · /model · /compact")
	}

	// Compose status line: left text + right-aligned info
	statusLine := renderStatusLine(leftStatus, rightInfo, m.width)

	return lipgloss.JoinVertical(lipgloss.Left,
		m.viewport.View(),
		statusLine,
		sep,
		m.textarea.View(),
	)
}

// renderStatusLine places left on the left and right on the right within width chars.
func renderStatusLine(left, right string, width int) string {
	// Strip ANSI to measure visible length
	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right)
	gap := width - leftLen - rightLen
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// rebuildViewport reassembles the viewport content from all state
func (m Model) rebuildViewport() Model {
	var sb strings.Builder

	// Completed history turns
	for _, turn := range m.history {
		// Show user message
		if turn.userMsg != "" {
			sb.WriteString(userMsgStyle.Render("> "+turn.userMsg) + "\n\n")
		}
		// Tool rows for this turn
		for _, row := range turn.toolRows {
			if row.hidden {
				continue
			}
			sb.WriteString(formatToolRow(row))
			sb.WriteString("\n")
		}
		if turn.rendered != "" {
			sb.WriteString(turn.rendered)
		}
		sb.WriteString("\n")
	}

	// Current in-progress turn
	if m.currentInput != "" {
		sb.WriteString(userMsgStyle.Render("> "+m.currentInput) + "\n\n")
	}
	if m.rendered != "" {
		// after EventMessage — rendered markdown
		for _, row := range m.toolRows {
			if row.hidden {
				continue
			}
			sb.WriteString(formatToolRow(row))
			sb.WriteString("\n")
		}
		sb.WriteString(m.rendered)
	} else if m.outputBuf.Len() > 0 {
		// streaming — raw text
		for _, row := range m.toolRows {
			if row.hidden {
				continue
			}
			sb.WriteString(formatToolRow(row))
			sb.WriteString("\n")
		}
		sb.WriteString(m.outputBuf.String())
	} else {
		// Only tool rows so far
		for _, row := range m.toolRows {
			if row.hidden {
				continue
			}
			sb.WriteString(formatToolRow(row))
			sb.WriteString("\n")
		}
	}

	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
	return m
}

// formatToolRow formats a single tool row line
func formatToolRow(row toolRow) string {
	var sb strings.Builder
	if row.done {
		if row.isError {
			sb.WriteString(toolErrStyle.Render("✗ " + row.name))
			if row.summary != "" {
				sb.WriteString(" " + row.summary)
			}
			if row.errMsg != "" {
				short := truncate80(strings.TrimPrefix(row.errMsg, "permission denied: "))
				sb.WriteString(toolErrStyle.Render(" [" + short + "]"))
			}
		} else {
			sb.WriteString(toolOkStyle.Render("✓ " + row.name))
			if row.summary != "" {
				sb.WriteString(" " + row.summary)
			}
		}
	} else {
		sb.WriteString(row.spinner.View())
		sb.WriteString(" ")
		sb.WriteString(toolSpinnerStyle.Render(row.name))
		if row.summary != "" {
			sb.WriteString(" " + statusStyle.Render(row.summary))
		}
	}
	return sb.String()
}

// toolSummaryFromInput extracts a short summary from tool input JSON
func toolSummaryFromInput(name string, input []byte) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	keys := map[string]string{
		"Bash":     "command",
		"Read":     "file_path",
		"Write":    "file_path",
		"Edit":     "file_path",
		"Glob":     "pattern",
		"Grep":     "pattern",
		"WebFetch": "url",
	}
	k, ok := keys[name]
	if !ok {
		for _, v := range m {
			var s string
			if json.Unmarshal(v, &s) == nil {
				return truncate80(s)
			}
		}
		return ""
	}
	raw, ok := m[k]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return truncate80(s)
}

func truncate80(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= 80 {
		return s
	}
	return s[:77] + "..."
}
