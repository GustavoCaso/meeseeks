package tui

import "github.com/charmbracelet/bubbles/key"

// GlobalKeyMap defines keys available in all tabs.
type GlobalKeyMap struct {
	Quit     key.Binding
	Tab      key.Binding
	ShiftTab key.Binding
}

// DefaultGlobalKeyMap returns the default global key bindings.
func DefaultGlobalKeyMap() GlobalKeyMap {
	return GlobalKeyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next tab"),
		),
		ShiftTab: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev tab"),
		),
	}
}

// NavigationKeyMap defines keys for list navigation.
type NavigationKeyMap struct {
	Up   key.Binding
	Down key.Binding
}

// DefaultNavigationKeyMap returns the default navigation key bindings.
func DefaultNavigationKeyMap() NavigationKeyMap {
	return NavigationKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
	}
}

// ProgramKeyMap defines keys for program management.
type ProgramKeyMap struct {
	Start   key.Binding
	Stop    key.Binding
	Restart key.Binding
	Logs    key.Binding
}

// DefaultProgramKeyMap returns the default program management key bindings.
func DefaultProgramKeyMap() ProgramKeyMap {
	return ProgramKeyMap{
		Start: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "start"),
		),
		Stop: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "stop"),
		),
		Restart: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "restart"),
		),
		Logs: key.NewBinding(
			key.WithKeys("l"),
			key.WithHelp("l", "logs"),
		),
	}
}

type LogsKeyMap struct {
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Top        key.Binding
	Bottom     key.Binding
	Follow     key.Binding
	Search     key.Binding
	Clear      key.Binding
	Escape     key.Binding
}

func NewLogsKeyMap() LogsKeyMap {
	return LogsKeyMap{
		ScrollUp: key.NewBinding(
			key.WithKeys("k", "pgup"),
			key.WithHelp("k", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("j", "pgdown"),
			key.WithHelp("j", "scroll down"),
		),
		Top: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G"),
			key.WithHelp("G", "bottom"),
		),
		Follow: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "follow"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Clear: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "clear"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	}
}
