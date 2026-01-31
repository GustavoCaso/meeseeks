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
	HelpBarStyleWidth = lipgloss.Width(HelpBarStyle.Render(""))

	HelpKeyStyle = lipgloss.NewStyle().
			Bold(true)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))
)

type footer struct {
	ctx     context.Context
	width   int
	height  int
	content string
}

func NewFooter(ctx context.Context) Component {
	return &footer{
		ctx:     ctx,
		width:   0,
		height:  0,
		content: "",
	}
}

func (f *footer) SetSize(width, height int) {
	f.width = width
	f.height = height
}

func (f *footer) Init() tea.Cmd {
	return nil
}

func (f *footer) updateContent() {
	var keys []string

	// Add tab-specific key bindings
	for _, binding := range f.ctx.HelpBindings {
		keys = append(keys, fmt.Sprintf("%s %s",
			HelpKeyStyle.Render(binding.Help().Key),
			HelpDescStyle.Render(binding.Help().Desc)))
	}

	// Add global keys
	keys = append(keys, fmt.Sprintf("%s %s",
		HelpKeyStyle.Render("q"),
		HelpDescStyle.Render("quit")))

	content := strings.Join(keys, " ")
	f.content = HelpBarStyle.Width(f.width - HelpBarStyleWidth).MaxHeight(f.height).Render(content)
}

func (f *footer) View() string {
	f.updateContent()
	return f.content
}

func (f *footer) Update(msg tea.Msg) (Component, tea.Cmd) {
	return f, nil
}

func (f *footer) SyncAppContext(ctx context.Context) {
	f.ctx = ctx
	f.updateContent()
}
