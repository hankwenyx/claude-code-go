package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap defines all keyboard bindings for the TUI
type keyMap struct {
	Submit      key.Binding
	Quit        key.Binding
	PermYes     key.Binding
	PermNo      key.Binding
	PermAlways  key.Binding
	PermSkip    key.Binding
	ScrollUp    key.Binding
	ScrollDown  key.Binding
	HistoryPrev key.Binding
	HistoryNext key.Binding
}

var keys = keyMap{
	Submit: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	PermYes: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "allow once"),
	),
	PermNo: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "deny"),
	),
	PermAlways: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "always allow"),
	),
	PermSkip: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "skip (deny)"),
	),
	ScrollUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup", "scroll up"),
	),
	ScrollDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("pgdown", "scroll down"),
	),
	HistoryPrev: key.NewBinding(
		key.WithKeys("ctrl+p", "up"),
		key.WithHelp("ctrl+p/↑", "previous input"),
	),
	HistoryNext: key.NewBinding(
		key.WithKeys("ctrl+n", "down"),
		key.WithHelp("ctrl+n/↓", "next input"),
	),
}
