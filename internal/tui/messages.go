package tui

import (
	"github.com/GustavoCaso/meeseeks/pkg/meeseeks"
)

// StatusUpdateMsg is sent when program statistics are fetched.
type StatusUpdateMsg struct {
	Statistics map[string]meeseeks.Statistics
	Err        error
}

// ClearStatusBarMsg is sent on an interval.
type ClearStatusBarMsg struct {
}

type InitialConfigLoadMsg struct {
	Err     error
	Content string
}

// LogLineMsg is sent when a new log line is received.
type LogLineMsg struct {
	Program string
	Line    string
	IsError bool
}

// ActionResultMsg is sent when a program action completes.
type ActionResultMsg struct {
	Action  string // "start", "stop", "restart"
	Program string
	Success bool
	Error   string
}

// ConfigLoadedMsg is sent when config file is loaded.
type ConfigLoadedMsg struct {
	Content string
	Path    string
	Err     error
}

// ConfigSavedMsg is sent when config is saved.
type ConfigSavedMsg struct {
	Success bool
	Error   string
}

// SwitchTabMsg requests switching to a specific tab.
type SwitchTabMsg struct {
	TabIndex int
	Program  string // Optional: pre-select program in target tab
}

// TickMsg is sent periodically to trigger status updates.
type TickMsg struct{}

// ClearStatusBarTickMsg is sent periodically to trigger clear status bar.
type ClearStatusBarTickMsg struct{}

// ErrorMsg represents an error that should be displayed.
type ErrorMsg struct {
	Error error
}

// LogStreamMsg is an internal message for log stream continuation.
type LogStreamMsg struct {
	Program     string
	Line        string
	IsError     bool
	StreamEnded bool
}
