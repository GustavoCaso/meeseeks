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
