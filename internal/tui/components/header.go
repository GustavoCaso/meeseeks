package components

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/GustavoCaso/meeseeks/internal/tui/context"
)

// Tab bar styles.
var (
	TabStyle = lipgloss.NewStyle().
			Padding(0, 1)

	ActiveTabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Underline(true)

	TabBarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			BorderLeft(true).
			BorderRight(true)

	TabBarStyleWidth  = lipgloss.Width(TabBarStyle.Render(""))
	TabBarStyleHeight = lipgloss.Height(TabBarStyle.Render(""))
)

type header struct {
	ctx     context.Context
	tabs    []string
	width   int
	height  int
	content string
}

func NewHeader(ctx context.Context, tabs []string) Component {
	return &header{
		ctx:     ctx,
		content: "",
		tabs:    tabs,
		width:   0,
		height:  0,
	}
}

func (h *header) SetSize(width, height int) {
	h.width = width
	h.height = height
}

func (h *header) Init() tea.Cmd {
	return nil
}

func (h *header) updateContent() {
	var tabs []string

	for i, title := range h.tabs {
		if i == h.ctx.ActiveTab {
			tabs = append(tabs, ActiveTabStyle.Render("["+title+"]"))
		} else {
			tabs = append(tabs, TabStyle.Render("["+title+"]"))
		}
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	h.content = TabBarStyle.Width(h.width - TabBarStyleWidth).MaxHeight(h.height).
		Render(content)
}

func (h *header) View() string {
	h.updateContent()
	return h.content
}

func (h *header) Update(msg tea.Msg) (Component, tea.Cmd) {
	return h, nil
}

func (h *header) SyncAppContext(ctx context.Context) {
	h.ctx = ctx
	h.updateContent()
}
