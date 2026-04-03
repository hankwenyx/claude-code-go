package tui

import "github.com/charmbracelet/lipgloss"

var (
	// statusStyle is for status/help line text
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// separatorStyle for the horizontal divider
	separatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("236"))

	// toolSpinnerStyle for running tool name
	toolSpinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))

	// toolOkStyle for completed tool name
	toolOkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))

	// toolErrStyle for errored tool name
	toolErrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	// permStyle highlights the permission prompt
	permStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)

	// permKeyStyle highlights the key options
	permKeyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	// errorStyle for fatal errors
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)

	// userMsgStyle for displaying the user's submitted message
	userMsgStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)

	// retryStyle for the retry countdown in the status bar
	retryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)
