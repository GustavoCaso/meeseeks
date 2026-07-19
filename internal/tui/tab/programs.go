package tab

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/GustavoCaso/meeseeks/internal/server"
	"github.com/GustavoCaso/meeseeks/internal/tui/messages"
	"github.com/GustavoCaso/meeseeks/internal/tui/styles"
	"github.com/GustavoCaso/meeseeks/pkg/meeseeks"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

type logEntry struct {
	content string
	isError bool
}

// logStreamMsg is an internal message for log stream continuation.
type logStreamMsg struct {
	Program     string
	Line        string
	IsError     bool
	StreamEnded bool
}

// logLineMsg is sent when a new log line is received.
type logLineMsg struct {
	Program string
	Line    string
	IsError bool
}

// navigationKeyMap defines keys for list navigation.
type navigationKeyMap struct {
	Up   key.Binding
	Down key.Binding
}

func newNavigationKeyMap() navigationKeyMap {
	return navigationKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
	}
}

// programKeyMap defines keys for program management.
type programKeyMap struct {
	Start   key.Binding
	Stop    key.Binding
	Restart key.Binding
	Logs    key.Binding
}

// newProgramKeyMap returns the default program management key bindings.
func newProgramKeyMap() programKeyMap {
	return programKeyMap{
		Start: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "start"),
		),
		Stop: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "stop"),
		),
		Restart: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "restart"),
		),
		Logs: key.NewBinding(
			key.WithKeys("l"),
			key.WithHelp("l", "logs"),
		),
	}
}

type logsKeyMap struct {
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Top        key.Binding
	Bottom     key.Binding
	Follow     key.Binding
	Search     key.Binding
	Clear      key.Binding
	Escape     key.Binding
}

func newLogsKeyMap() logsKeyMap {
	return logsKeyMap{
		ScrollUp: key.NewBinding(
			key.WithKeys("k", "pgup"),
			key.WithHelp("k", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("j", "pgdown"),
			key.WithHelp("j", "scroll down"),
		),
		Top: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G"),
			key.WithHelp("G", "bottom"),
		),
		Follow: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "follow"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Clear: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "clear"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
	}
}

// Programs displays detailed program info with management controls.
type Programs struct {
	client     *server.Client
	programs   []string
	selected   int
	statistics map[string]meeseeks.Statistics

	width    int
	height   int
	navKeys  navigationKeyMap
	progKeys programKeyMap
	err      error

	showLogs     bool
	following    bool
	cancelFunc   context.CancelFunc // To cancel log streaming
	logCh        chan []byte        // Channel for receiving log lines
	logs         []logEntry
	logsKeys     logsKeyMap
	logsViewport viewport.Model
	searchMode   bool
	searchQuery  string
}

// NewPrograms creates a new Programs tab.
func NewPrograms(client *server.Client) Tab {
	return &Programs{
		client:     client,
		statistics: make(map[string]meeseeks.Statistics),
		programs:   []string{},
		navKeys:    newNavigationKeyMap(),
		progKeys:   newProgramKeyMap(),
		logsKeys:   newLogsKeyMap(),
	}
}

func (p *Programs) Init() tea.Cmd {
	return nil
}

func (p *Programs) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case messages.StatusUpdateMsg:
		if msg.Err != nil {
			p.err = msg.Err
			return p, nil
		}
		p.err = nil
		p.statistics = msg.Statistics
		p.programs = make([]string, 0, len(msg.Statistics))
		for name := range msg.Statistics {
			p.programs = append(p.programs, name)
		}
		sort.Strings(p.programs)
		if p.selected >= len(p.programs) {
			p.selected = max(0, len(p.programs)-1)
		}

		return p, nil

	case messages.SwitchTabMsg:
		// Pre-select program if specified
		if msg.Program != "" {
			for i, name := range p.programs {
				if name == msg.Program {
					p.selected = i
					break
				}
			}
		}
		return p, nil

	case logStreamMsg:
		return p.handlelogStreamMsg(msg)
	case logLineMsg:
		return p.handleLogLine(msg)
	case tea.KeyMsg:
		return p.handleKeyMsg(msg)
	}
	return p, nil
}

func (p *Programs) handleKeyMsg(msg tea.KeyMsg) (Tab, tea.Cmd) {
	if p.searchMode {
		return p.handleSearchInput(msg)
	}

	if p.showLogs {
		return p.handleLogInput(msg)
	}

	switch {
	case key.Matches(msg, p.navKeys.Up):
		if p.selected > 0 {
			p.selected--
		}
	case key.Matches(msg, p.navKeys.Down):
		if p.selected < len(p.programs)-1 {
			p.selected++
		}
	case key.Matches(msg, p.progKeys.Start):
		if len(p.programs) > 0 {
			return p, p.startProgram(p.programs[p.selected])
		}
	case key.Matches(msg, p.progKeys.Stop):
		if len(p.programs) > 0 {
			return p, p.stopProgram(p.programs[p.selected])
		}
	case key.Matches(msg, p.progKeys.Restart):
		if len(p.programs) > 0 {
			return p, p.restartProgram(p.programs[p.selected])
		}
	case key.Matches(msg, p.progKeys.Logs):
		p.showLogs = !p.showLogs
		return p, p.startLogStream()
	}

	return p, nil
}

func (p *Programs) handleSearchInput(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch {
	case key.Matches(msg, p.logsKeys.Escape):
		p.searchMode = false
		p.searchQuery = ""
		p.updateLogsViewport()
	case msg.Type == tea.KeyEnter:
		p.searchMode = false
		p.updateLogsViewport()
	case msg.Type == tea.KeyBackspace:
		if len(p.searchQuery) > 0 {
			p.searchQuery = p.searchQuery[:len(p.searchQuery)-1]
			p.updateLogsViewport()
		}
	case msg.Type == tea.KeyRunes:
		p.searchQuery += string(msg.Runes)
		p.updateLogsViewport()
	}
	return p, nil
}

func (p *Programs) handleLogInput(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch {
	case key.Matches(msg, p.logsKeys.Escape):
		p.showLogs = false
		return p, nil
	case key.Matches(msg, p.navKeys.Up):
		if p.selected > 0 {
			p.selected--
			p.logs = []logEntry{}
			return p, p.startLogStream()
		}
	case key.Matches(msg, p.navKeys.Down):
		if p.selected < len(p.programs)-1 {
			p.selected++
			p.logs = []logEntry{}
			return p, p.startLogStream()
		}
	case key.Matches(msg, p.progKeys.Logs):
		p.showLogs = !p.showLogs
	case key.Matches(msg, p.logsKeys.ScrollUp):
		p.following = false
		p.logsViewport.ScrollUp(1)
	case key.Matches(msg, p.logsKeys.ScrollDown):
		p.logsViewport.ScrollDown(1)
	case key.Matches(msg, p.logsKeys.Top):
		p.following = false
		p.logsViewport.GotoTop()
	case key.Matches(msg, p.logsKeys.Bottom):
		p.logsViewport.GotoBottom()
		p.following = true
	case key.Matches(msg, p.logsKeys.Follow):
		p.following = !p.following
		if p.following {
			p.logsViewport.GotoBottom()
		}
	case key.Matches(msg, p.logsKeys.Search):
		p.searchMode = true
		p.searchQuery = ""
	case key.Matches(msg, p.logsKeys.Clear):
		p.logs = []logEntry{}
		p.updateLogsViewport()
	}
	return p, nil
}

func (p *Programs) View() string {
	if p.err != nil {
		return styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", p.err))
	}

	if len(p.programs) == 0 {
		return styles.IdleStyle.Render("No programs configured")
	}

	// Split view: left panel (program list), right panel (details)
	leftWidth, rightWidth := getPanelsWidth(p.width)

	leftPanel := p.renderProgramList(leftWidth)
	rightPanel := p.renderDetails()

	// Join panels
	left := styles.BorderStyle.Width(leftWidth - styles.BorderStyleWidth).
		Height(p.height - styles.BorderStyleHeight).
		Render(leftPanel)
	right := styles.BorderStyle.Width(rightWidth - styles.BorderStyleWidth).
		Height(p.height - styles.BorderStyleHeight).
		Render(rightPanel)

	content := lipgloss.JoinHorizontal(lipgloss.Left, left, right)

	return content
}

func (p *Programs) renderProgramList(width int) string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("PROGRAMS"))
	b.WriteString("\n\n")

	for i, name := range p.programs {
		line := truncate(name, width-4)
		if i == p.selected {
			line = "> " + line
			b.WriteString(styles.SelectedStyle.Render(line))
		} else {
			line = "  " + line
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (p *Programs) renderDetails() string {
	if p.selected >= len(p.programs) {
		return ""
	}

	if p.showLogs {
		return p.renderLogView()
	}

	name := p.programs[p.selected]
	stats := p.statistics[name]

	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("DETAILS"))
	b.WriteString("\n\n")

	// Program details
	fmt.Fprintf(&b, "  Name:       %s\n", stats.ProgramName)
	fmt.Fprintf(&b, "  Status:     %s\n", p.statusIndicator(stats.State))

	interval := "none"
	if stats.Interval != "" {
		interval = stats.Interval
	}
	fmt.Fprintf(&b, "  Interval:   %s\n", interval)
	b.WriteString("\n")

	fmt.Fprintf(&b, "  Successful: %d\n", stats.Successful)
	fmt.Fprintf(&b, "  Failed:     %d\n", stats.Failed)
	fmt.Fprintf(&b, "  Retries:    %d\n", stats.Retries)
	fmt.Fprintf(&b, "  Last Run:   %s\n", stats.LastRunAt)

	if stats.NextRunAt != "" {
		fmt.Fprintf(&b, "  Next Run:   %s\n", stats.NextRunAt)
	}

	return b.String()
}

func (p *Programs) logsHeader() string {
	var b strings.Builder

	programName := ""
	if p.selected < len(p.programs) {
		programName = p.programs[p.selected]
	}

	title := fmt.Sprintf("LOGS: %s", programName)
	b.WriteString(styles.TitleStyle.Render(title))

	// Following indicator
	if p.following {
		b.WriteString(styles.IdleStyle.Render("  ▼ following"))
	}
	b.WriteString("\n\n")

	// Search bar if in search mode
	if p.searchMode {
		fmt.Fprintf(&b, "Search: %s█\n\n", p.searchQuery)
	} else if p.searchQuery != "" {
		b.WriteString(styles.IdleStyle.Render(fmt.Sprintf("Filter: %s\n\n", p.searchQuery)))
	}
	return b.String()
}

func (p *Programs) renderLogView() string {
	return p.logsHeader() + p.logsViewport.View()
}

func (p *Programs) statusIndicator(state string) string {
	switch state {
	case "running":
		return styles.RunningStyle.Render("● running")
	case "error":
		return styles.ErrorStyle.Render("✗ error")
	case "cancelled":
		return styles.StoppedStyle.Render("○ stopped")
	case "finished":
		return styles.RunningStyle.Render("finished")
	default:
		return styles.IdleStyle.Render("○ idle")
	}
}

func (p *Programs) startProgram(name string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		_, err := p.client.RunProgram(ctx, name, true)
		if err != nil {
			return messages.SetStatusBarMsg{
				Err: err,
			}
		}
		return messages.SetStatusBarMsg{
			Message: fmt.Sprintf("%s started successfully", name),
		}
	}
}

func (p *Programs) stopProgram(name string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		_, err := p.client.Stop(ctx, name, "5s")
		if err != nil {
			return messages.SetStatusBarMsg{
				Err: err,
			}
		}
		return messages.SetStatusBarMsg{
			Message: fmt.Sprintf("%s stopped successfully", name),
		}
	}
}

func (p *Programs) restartProgram(name string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		// Stop first
		_, err := p.client.Stop(ctx, name, "5s")
		if err != nil {
			return messages.SetStatusBarMsg{
				Err: err,
			}
		}

		// Wait briefly for shutdown
		time.Sleep(100 * time.Millisecond)

		// Then start
		_, err = p.client.RunProgram(ctx, name, true)
		if err != nil {
			return messages.SetStatusBarMsg{
				Err: err,
			}
		}
		return messages.SetStatusBarMsg{
			Message: fmt.Sprintf("%s restarted successfully", name),
		}
	}
}

func (p *Programs) startLogStream() tea.Cmd {
	if p.selected >= len(p.programs) {
		return nil
	}

	programName := p.programs[p.selected]

	if p.cancelFunc != nil {
		p.cancelFunc()
	}

	//nolint:gosec // context cancellation function is called
	ctx, cancel := context.WithCancel(context.Background())

	p.cancelFunc = cancel

	// Create a new channel for this stream
	p.logCh = make(chan []byte)
	return func() tea.Msg {
		err := p.client.FollowLogs(ctx, programName, true, p.logCh)
		if err != nil {
			return messages.ErrorMsg{Error: err}
		}

		// Read the first log line (blocking)
		line, ok := <-p.logCh
		if !ok {
			return logStreamMsg{Program: programName, StreamEnded: true}
		}

		// Parse the JSON log line
		var logLine program.LogLine
		if unmarshalErr := json.Unmarshal(line, &logLine); unmarshalErr != nil {
			// If parsing fails, use raw line
			return logStreamMsg{
				Program: programName,
				Line:    string(line),
				IsError: false,
			}
		}

		return logStreamMsg{
			Program: programName,
			Line:    logLine.Message,
			IsError: logLine.IsError,
		}
	}
}

func (p *Programs) handlelogStreamMsg(msg logStreamMsg) (Tab, tea.Cmd) {
	// Check if this message is for the currently selected program
	if p.selected < len(p.programs) && p.programs[p.selected] != msg.Program {
		// Ignore messages from a different program (user switched)
		return p, nil
	}

	if msg.StreamEnded {
		// Stream ended, no more commands to return
		return p, nil
	}

	// Add the log entry
	p.logs = append(p.logs, logEntry{
		content: msg.Line,
		isError: msg.IsError,
	})

	p.updateLogsViewport()

	if p.following {
		p.logsViewport.GotoBottom()
	}

	// Return command to continue reading from the channel
	return p, p.waitForNextLogLine(msg.Program)
}

func (p *Programs) handleLogLine(msg logLineMsg) (Tab, tea.Cmd) {
	p.logs = append(p.logs, logEntry{
		content: msg.Line,
		isError: msg.IsError,
	})
	p.updateLogsViewport()
	if p.following {
		p.logsViewport.GotoBottom()
	}
	return p, nil
}

// waitForNextLogLine returns a command that waits for the next log line from the channep.
func (p *Programs) waitForNextLogLine(program string) tea.Cmd {
	return func() tea.Msg {
		if p.logCh == nil {
			return logStreamMsg{Program: program, StreamEnded: true}
		}

		line, ok := <-p.logCh
		if !ok {
			return logStreamMsg{Program: program, StreamEnded: true}
		}

		// Parse the JSON log line
		var logData struct {
			Message string `json:"message"`
			IsError bool   `json:"is_error"`
		}
		if unmarshalErr := json.Unmarshal(line, &logData); unmarshalErr != nil {
			return logStreamMsg{
				Program: program,
				Line:    string(line),
				IsError: false,
			}
		}

		return logStreamMsg{
			Program: program,
			Line:    logData.Message,
			IsError: logData.IsError,
		}
	}
}

func (p *Programs) updateLogsViewport() {
	var content strings.Builder

	for _, entry := range p.logs {
		line := entry.content

		// Apply search filter
		if p.searchQuery != "" &&
			!strings.Contains(strings.ToLower(line), strings.ToLower(p.searchQuery)) {
			continue
		}

		if entry.isError {
			content.WriteString(styles.ErrorStyle.Render(line))
		} else {
			content.WriteString(line)
		}
		content.WriteString("\n")
	}

	p.logsViewport.SetContent(content.String())
}

func (p *Programs) Title() string {
	return "Programs"
}

func (p *Programs) ShortHelp() []key.Binding {
	if p.showLogs {
		if p.searchMode {
			return []key.Binding{
				p.logsKeys.Escape,
			}
		}
		return []key.Binding{
			p.logsKeys.Escape,
			p.navKeys.Up,
			p.navKeys.Down,
			p.logsKeys.Top,
			p.logsKeys.Bottom,
			p.logsKeys.ScrollUp,
			p.logsKeys.ScrollDown,
			p.logsKeys.Follow,
			p.logsKeys.Search,
			p.logsKeys.Clear,
		}
	}

	return []key.Binding{
		p.navKeys.Up,
		p.navKeys.Down,
		p.progKeys.Start,
		p.progKeys.Stop,
		p.progKeys.Restart,
		p.progKeys.Logs,
	}
}

func (p *Programs) SetSize(width, height int) {
	p.width = width
	p.height = height

	_, rightPanelWidth := getPanelsWidth(width)

	// Right panel is 3/4 of width, minus border
	vpWidth := max(1, rightPanelWidth-styles.BorderStyleWidth)
	// Height minus border and logs header
	logsHeaderHeight := lipgloss.Height(p.logsHeader())
	logsHeaderHeight += 1 // We need. to account for the case that we search for logs. We add an extra line
	vpHeight := max(1, height-styles.BorderStyleHeight-logsHeaderHeight)

	vp := viewport.New(vpWidth, vpHeight)
	vp.SetContent("")
	p.logsViewport = vp
}
