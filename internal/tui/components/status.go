package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/GustavoCaso/meeseeks/internal/tui/context"
	"github.com/GustavoCaso/meeseeks/internal/tui/messages"
)

// Status bar styles.
var (
	StatusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#6124DF"))

	StatusMessageStyle = lipgloss.NewStyle().
				Inherit(StatusBarStyle).
				Padding(0, 1)

	StatusErrorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFDF5")).
				Background(lipgloss.Color("#f62206"))

	VersionStyle = lipgloss.NewStyle().
			Inherit(StatusBarStyle).
			Padding(0, 1).
			Bold(true)
)

type status struct {
	ctx           context.Context
	width         int
	height        int
	statusMessage string
	version       string
}

func NewStatus(ctx context.Context, version string) Component {
	return &status{
		ctx:           ctx,
		version:       version,
		statusMessage: "",
		width:         0,
		height:        0,
	}
}

func (s *status) SetSize(width, height int) {
	s.width = width
	s.height = height
}

func (s *status) Init() tea.Cmd {
	return nil
}

func (s *status) View() string {
	// Render version on the right
	versionText := VersionStyle.Render("meeseeks version:" + s.version)
	versionWidth := lipgloss.Width(versionText)

	// Render status message on the left
	statusText := StatusMessageStyle.Render(s.statusMessage)

	// Calculate available space for the middle gap
	availableWidth := max(0, s.width-versionWidth-lipgloss.Width(statusText))

	// Create the gap with the status bar background
	gap := StatusBarStyle.Render(strings.Repeat(" ", availableWidth))

	// Join: status message (left) + gap (middle) + version (right)
	return lipgloss.JoinHorizontal(lipgloss.Top, statusText, gap, versionText)
}

func (s *status) Update(msg tea.Msg) (Component, tea.Cmd) {
	switch msg := msg.(type) {
	case messages.SetStatusBarMsg:
		if msg.Err != nil {
			s.statusMessage = StatusErrorStyle.Render(msg.Err.Error())
		} else {
			s.statusMessage = msg.Message
		}
		return s, nil
	case messages.ClearStatusBarMsg:
		s.statusMessage = ""
		return s, nil
	}
	return s, nil
}

func (s *status) SyncAppContext(ctx context.Context) {
	s.ctx = ctx
}
