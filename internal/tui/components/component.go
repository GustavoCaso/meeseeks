package components

import (
	"github.com/GustavoCaso/meeseeks/internal/tui/context"

	tea "github.com/charmbracelet/bubbletea"
)

type Component interface {
	Init() tea.Cmd

	Update(msg tea.Msg) (Component, tea.Cmd)

	View() string

	SyncAppContext(ctx context.Context)

	SetSize(width, height int)
}
