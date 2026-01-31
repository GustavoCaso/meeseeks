package tui

import "github.com/charmbracelet/lipgloss"

// Colors - minimal palette with status indicators only.
var (
	ColorRunning = lipgloss.Color("2") // Green
	ColorError   = lipgloss.Color("1") // Red
	ColorStopped = lipgloss.Color("3") // Yellow
	ColorDim     = lipgloss.Color("8") // Gray
	ColorCyan    = lipgloss.Color("6") // Cyan for numbers
)

// Content area styles.
var (
	ContentStyle = lipgloss.NewStyle()

	BorderStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8"))

	BorderStyleWidth  = lipgloss.Width(BorderStyle.Render(""))
	BorderStyleHeight = lipgloss.Height(BorderStyle.Render(""))

	TitleStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true)
)

// Status indicator styles.
var (
	RunningStyle = lipgloss.NewStyle().
			Foreground(ColorRunning)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError)

	StoppedStyle = lipgloss.NewStyle().
			Foreground(ColorStopped)

	IdleStyle = lipgloss.NewStyle().
			Foreground(ColorDim)

	StatusBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4"))
)

// Selected row style.
var (
	SelectedStyle = lipgloss.NewStyle().
		Bold(true).
		Background(lipgloss.Color("8")).
		Foreground(lipgloss.Color("15"))
)

// Syntax highlighting styles for YAML.
var (
	SyntaxKeyStyle = lipgloss.NewStyle().
			Foreground(ColorCyan)

	SyntaxStringStyle = lipgloss.NewStyle().
				Foreground(ColorRunning)

	SyntaxCommentStyle = lipgloss.NewStyle().
				Foreground(ColorDim)

	SyntaxNumberStyle = lipgloss.NewStyle().
				Foreground(ColorStopped)
)
