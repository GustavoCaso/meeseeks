package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Tab defines the interface that all tabs must implement.
type Tab interface {
	// Init initializes the tab and returns any initial commands
	Init() tea.Cmd

	// Update handles messages and returns the updated tab and any commands
	Update(msg tea.Msg) (Tab, tea.Cmd)

	// View renders the tab content
	View() string

	// Title returns the tab title for the tab bar
	Title() string

	// ShortHelp returns key bindings to display in help bar
	ShortHelp() []key.Binding

	// SetSize updates the tab dimensions
	SetSize(width, height int)
}

// TabIndex constants for tab navigation.
const (
	TabPrograms = iota
	TabLogs
	TabConfig
)
