package tabs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gopkg.in/yaml.v3"

	"github.com/GustavoCaso/meeseeks/internal/server"
	"github.com/GustavoCaso/meeseeks/internal/tui"
)

type configKeyMap struct {
	Edit       key.Binding
	Save       key.Binding
	Refresh    key.Binding
	Undo       key.Binding
	Escape     key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
}

func newConfigKeyMap() configKeyMap {
	return configKeyMap{
		Edit: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit"),
		),
		Save: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "save"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Undo: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "undo"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("k"),
			key.WithHelp("k", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("j"),
			key.WithHelp("j", "scroll down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdn", "page down"),
		),
	}
}

// Config provides a YAML editor for the config file.
type Config struct {
	client       *server.Client
	configPath   string
	textarea     textarea.Model
	viewport     viewport.Model
	originalText string
	modified     bool
	editMode     bool
	width        int
	height       int
	configKeys   configKeyMap
	message      string
	err          error
}

// NewConfig creates a new Config tab.
func NewConfig(client *server.Client, configPath string) *Config {
	ta := textarea.New()
	ta.ShowLineNumbers = true

	vp := viewport.New(0, 0)

	config := &Config{
		client:     client,
		configPath: configPath,
		textarea:   ta,
		viewport:   vp,
		configKeys: newConfigKeyMap(),
	}

	return config
}

func (c *Config) Init() tea.Cmd {
	return c.initLoadConfig()
}

//nolint:funlen //bubbletea Update function normally has many statements
func (c *Config) Update(msg tea.Msg) (tui.Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case tui.ClearStatusBarMsg:
		c.message = ""
		return c, nil
	case tui.ConfigLoadedMsg:
		if msg.Err != nil {
			c.err = msg.Err
			return c, nil
		}
		c.err = nil
		c.originalText = msg.Content
		c.textarea.SetValue(msg.Content)
		c.viewport.SetContent(highlightYAML(msg.Content))
		c.modified = false
		c.configPath = msg.Path
		return c, nil

	case tui.InitialConfigLoadMsg:
		if msg.Err != nil {
			c.err = msg.Err
			return c, nil
		}
		c.err = nil
		c.originalText = msg.Content
		c.textarea.SetValue(msg.Content)
		c.viewport.SetContent(highlightYAML(msg.Content))
		c.modified = false
		return c, nil

	case tui.ConfigSavedMsg:
		if msg.Success {
			c.message = "Config saved and reloaded"
			c.originalText = c.textarea.Value()
			c.modified = false
			c.viewport.SetContent(highlightYAML(c.originalText))
		} else {
			c.message = fmt.Sprintf("Save failed: %s", msg.Error)
		}
		return c, nil

	case tea.KeyMsg:
		if c.editMode {
			return c.handleEditMode(msg)
		}

		switch {
		case key.Matches(msg, c.configKeys.Edit):
			c.editMode = true
			c.textarea.Focus()
			c.message = ""
			return c, textarea.Blink

		case key.Matches(msg, c.configKeys.Save):
			return c, c.saveConfig()

		case key.Matches(msg, c.configKeys.Refresh):
			c.message = ""
			return c, c.loadConfig()

		case key.Matches(msg, c.configKeys.Undo):
			c.textarea.SetValue(c.originalText)
			c.viewport.SetContent(highlightYAML(c.originalText))
			c.modified = false
			c.message = "Changes reverted"

		case key.Matches(msg, c.configKeys.ScrollUp):
			c.viewport.ScrollUp(1)

		case key.Matches(msg, c.configKeys.ScrollDown):
			c.viewport.ScrollDown(1)

		case key.Matches(msg, c.configKeys.PageUp):
			c.viewport.HalfPageUp()

		case key.Matches(msg, c.configKeys.PageDown):
			c.viewport.HalfPageDown()
		}
	}

	return c, nil
}

func (c *Config) handleEditMode(msg tea.KeyMsg) (tui.Tab, tea.Cmd) {
	if key.Matches(msg, c.configKeys.Escape) {
		c.editMode = false
		c.textarea.Blur()
		// Update viewport with current content (possibly modified)
		c.viewport.SetContent(highlightYAML(c.textarea.Value()))
		return c, nil
	}

	var cmd tea.Cmd
	c.textarea, cmd = c.textarea.Update(msg)
	c.modified = c.textarea.Value() != c.originalText
	return c, cmd
}

func (c *Config) configHeader() string {
	var b strings.Builder

	// Header with file path
	header := fmt.Sprintf("CONFIG: %s", c.configPath)
	b.WriteString(tui.TitleStyle.Render(header))

	// Modified indicator
	if c.modified {
		b.WriteString(tui.ErrorStyle.Render("  [modified]"))
	}

	// Edit mode indicator
	if c.editMode {
		b.WriteString(tui.RunningStyle.Render("  [editing]"))
	}
	b.WriteString("\n\n")

	// Error display
	if c.err != nil {
		b.WriteString(tui.ErrorStyle.Render(fmt.Sprintf("Error: %v\n\n", c.err)))
	}

	return b.String()
}

func (c *Config) configFooter() string {
	var b strings.Builder

	contentWidth := c.width - tui.BorderStyleWidth

	// Show cursor/scroll position
	if c.editMode {
		row, col := c.textarea.Line(), c.textarea.LineInfo().ColumnOffset
		b.WriteString(
			tui.StatusBarStyle.
				Width(contentWidth).
				AlignHorizontal(lipgloss.Right).
				Render(fmt.Sprintf("L:%d C:%d", row+1, col+1)),
		)
	}

	// Message
	if c.message != "" {
		b.WriteString(
			tui.StatusBarStyle.
				Width(contentWidth).
				AlignHorizontal(lipgloss.Right).
				Render(c.message),
		)
	}

	return b.String()
}

func (c *Config) renderContent() string {
	if c.editMode {
		return c.textarea.View()
	}
	return c.viewport.View()
}

func (c *Config) View() string {
	contentWidth := c.width - tui.BorderStyleWidth

	content := tui.BorderStyle.Width(contentWidth).
		Height(c.height - tui.BorderStyleHeight).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Left,
				c.configHeader(),
				c.renderContent(),
				c.configFooter(),
			),
		)

	return content
}

func (c *Config) initLoadConfig() tea.Cmd {
	return func() tea.Msg {
		content, err := os.ReadFile(c.configPath)
		if err != nil {
			return tui.InitialConfigLoadMsg{Err: err}
		}
		return tui.InitialConfigLoadMsg{
			Content: string(content),
		}
	}
}

func (c *Config) loadConfig() tea.Cmd {
	return func() tea.Msg {
		content, err := os.ReadFile(c.configPath)
		if err != nil {
			return tui.ConfigLoadedMsg{Err: err}
		}
		return tui.ConfigLoadedMsg{
			Content: string(content),
			Path:    c.configPath,
		}
	}
}

func (c *Config) saveConfig() tea.Cmd {
	return func() tea.Msg {
		content := c.textarea.Value()

		// Validate YAML syntax
		var parsed any
		if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
			return tui.ConfigSavedMsg{
				Success: false,
				Error:   fmt.Sprintf("invalid YAML: %v", err),
			}
		}

		// Write to file
		if err := os.WriteFile(c.configPath, []byte(content), 0600); err != nil {
			return tui.ConfigSavedMsg{
				Success: false,
				Error:   fmt.Sprintf("write failed: %v", err),
			}
		}

		// Trigger daemon reload
		ctx := context.Background()
		_, err := c.client.Reload(ctx, "5s")
		if err != nil {
			return tui.ConfigSavedMsg{
				Success: false,
				Error:   fmt.Sprintf("reload failed: %v", err),
			}
		}

		return tui.ConfigSavedMsg{Success: true}
	}
}

func (c *Config) Title() string {
	return "Config"
}

func (c *Config) ShortHelp() []key.Binding {
	if c.editMode {
		return []key.Binding{
			c.configKeys.Escape,
		}
	}
	return []key.Binding{
		c.configKeys.Edit,
		c.configKeys.Save,
		c.configKeys.Refresh,
		c.configKeys.ScrollUp,
		c.configKeys.ScrollDown,
		c.configKeys.Undo,
	}
}

func (c *Config) SetSize(width, height int) {
	c.width = width
	c.height = height

	// Content width: full width minus border
	contentWidth := max(1, width-tui.BorderStyleWidth)
	// Content height: full height minus border, header, and footer
	headerHeight := lipgloss.Height(c.configHeader())
	footerHeight := lipgloss.Height(c.configFooter())
	contentHeight := max(1, height-tui.BorderStyleHeight-headerHeight-footerHeight)

	c.textarea.SetWidth(contentWidth)
	c.textarea.SetHeight(contentHeight)

	vp := viewport.New(contentWidth, contentHeight)
	vp.SetContent(c.viewport.View())
	c.viewport = vp
}

// highlightYAML applies syntax highlighting to YAML content using Chroma.
func highlightYAML(content string) string {
	var buf bytes.Buffer
	err := quick.Highlight(&buf, content, "yaml", "terminal256", "solarized-dark")
	if err != nil {
		return content
	}

	return buf.String()
}
