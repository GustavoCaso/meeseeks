package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/GustavoCaso/meeseeks/internal/tui/context"
)

// Footer help bar styles.
var (
	HelpBarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			Foreground(lipgloss.Color("8"))
	HelpKeyStyle  = lipgloss.NewStyle().Bold(true)
	HelpDescStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type help struct {
	ctx     context.Context
	width   int
	height  int
	content string
}

func NewHelp(ctx context.Context) Component {
	return &help{
		ctx:     ctx,
		width:   0,
		height:  0,
		content: "",
	}
}

func (h *help) SetSize(width, height int) {
	h.width = width
	h.height = height
}

func (h *help) Init() tea.Cmd {
	return nil
}

func (h *help) updateContent() {
	var keys []string

	// Add tab-specific key bindings
	for _, binding := range h.ctx.HelpBindings {
		keys = append(keys, fmt.Sprintf("%s %s",
			HelpKeyStyle.Render(binding.Help().Key),
			HelpDescStyle.Render(binding.Help().Desc)))
	}

	// Add global keys
	keys = append(keys, fmt.Sprintf("%s %s",
		HelpKeyStyle.Render("q"),
		HelpDescStyle.Render("quit")))

	h.content = HelpBarStyle.Width(h.width).
		Height(h.height).
		Render(strings.Join(keys, "  "))
}

func (h *help) View() string {
	h.updateContent()
	return h.content
}

func (h *help) Update(msg tea.Msg) (Component, tea.Cmd) {
	return h, nil
}

func (h *help) SyncAppContext(ctx context.Context) {
	h.ctx = ctx
	h.updateContent()
}
