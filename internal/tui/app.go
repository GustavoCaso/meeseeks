package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tuiContext "github.com/GustavoCaso/meeseeks/internal/tui/context"
	"github.com/GustavoCaso/meeseeks/internal/tui/messages"
	"github.com/GustavoCaso/meeseeks/internal/tui/styles"
	"github.com/GustavoCaso/meeseeks/internal/tui/tab"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/GustavoCaso/meeseeks/internal/server"
	"github.com/GustavoCaso/meeseeks/internal/tui/components"
	"github.com/GustavoCaso/meeseeks/pkg/meeseeks"
)

const tickInterval = 1 * time.Second
const statusBarTickInterval = 2 * time.Second

// App is the main TUI application model.
type App struct {
	client     *server.Client
	configPath string
	tabs       []tab.Tab
	activeTab  int
	ctx        tuiContext.Context
	header     components.Component
	status     components.Component
	help       components.Component
	width      int
	height     int
	globalKeys GlobalKeyMap
	err        error
	quitting   bool
}

// NewApp creates a new App instance.
func NewApp(client *server.Client, configPath string, tabs []tab.Tab, version string) *App {
	ctx := tuiContext.Context{}
	tabNames := make([]string, len(tabs))
	for i, tab := range tabs {
		tabNames[i] = tab.Title()
	}

	return &App{
		client:     client,
		configPath: configPath,
		tabs:       tabs,
		activeTab:  0,
		ctx:        ctx,
		header:     components.NewHeader(ctx, tabNames),
		status:     components.NewStatus(ctx, version),
		help:       components.NewHelp(ctx),
		globalKeys: DefaultGlobalKeyMap(),
	}
}

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.tick(),
		a.clearStatusBarTick(),
		a.fetchStatus(),
	}

	// Initialize all tabs
	for _, tab := range a.tabs {
		if cmd := tab.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return a.handleWindowSize(msg)
	case messages.TickMsg:
		return a, tea.Batch(a.tick(), a.fetchStatus())
	case messages.ClearStatusBarTickMsg:
		return a, tea.Batch(a.clearStatusBarTick(), a.clearStatus())
	case messages.SwitchTabMsg:
		return a.handleSwitchTab(msg)
	case messages.SetStatusBarMsg:
		return a.handleSetStatus(msg)
	case messages.ClearStatusBarMsg:
		return a.handleClearStatus(msg)
	case messages.ErrorMsg:
		a.err = msg.Error
		return a, nil
	case tea.KeyMsg:
		if model, cmd, handled := a.handleGlobalKeys(msg); handled {
			a.syncContext()
			return model, cmd
		}
	}

	a.syncContext()

	return a.forwardToTabs(msg)
}

func (a *App) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	a.width = msg.Width
	a.height = msg.Height

	headerHeight := lipgloss.Height(a.header.View())
	statusHeight := lipgloss.Height(a.status.View())
	helpHeight := 1 // We do not want to help to take more space than one
	contentHeight := a.height - headerHeight - statusHeight - helpHeight

	a.header.SetSize(a.width, headerHeight)
	a.status.SetSize(a.width, statusHeight)
	a.help.SetSize(a.width, helpHeight)

	for _, tab := range a.tabs {
		tab.SetSize(a.width, contentHeight)
	}
	return a, nil
}

func (a *App) handleSwitchTab(msg messages.SwitchTabMsg) (tea.Model, tea.Cmd) {
	a.activeTab = msg.TabIndex
	if a.activeTab < len(a.tabs) {
		tab, cmd := a.tabs[a.activeTab].Update(msg)
		a.tabs[a.activeTab] = tab
		return a, cmd
	}
	return a, nil
}

func (a *App) handleSetStatus(msg messages.SetStatusBarMsg) (tea.Model, tea.Cmd) {
	status, cmd := a.status.Update(msg)
	a.status = status

	return a, cmd
}

func (a *App) handleClearStatus(msg messages.ClearStatusBarMsg) (tea.Model, tea.Cmd) {
	status, cmd := a.status.Update(msg)
	a.status = status

	return a, cmd
}

func (a *App) syncContext() {
	a.ctx.ActiveTab = a.activeTab
	if a.activeTab < len(a.tabs) {
		a.ctx.HelpBindings = a.tabs[a.activeTab].ShortHelp()
	}
	a.header.SyncAppContext(a.ctx)
	a.status.SyncAppContext(a.ctx)
	a.help.SyncAppContext(a.ctx)
}

func (a *App) handleGlobalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, a.globalKeys.Quit):
		a.quitting = true
		return a, tea.Quit, true
	case key.Matches(msg, a.globalKeys.Tab):
		a.activeTab = (a.activeTab + 1) % len(a.tabs)
		return a, nil, true
	case key.Matches(msg, a.globalKeys.ShiftTab):
		a.activeTab = (a.activeTab - 1 + len(a.tabs)) % len(a.tabs)
		return a, nil, true
	}
	return nil, nil, false
}

func (a *App) forwardToTabs(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Forward message to active tab
	if a.activeTab < len(a.tabs) {
		tab, cmd := a.tabs[a.activeTab].Update(msg)
		a.tabs[a.activeTab] = tab
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	_, initConfigMsg := msg.(messages.InitialConfigLoadMsg)
	_, statusUpdateMsg := msg.(messages.StatusUpdateMsg)

	if initConfigMsg || statusUpdateMsg {
		for i, tab := range a.tabs {
			if i != a.activeTab {
				updatedTab, cmd := tab.Update(msg)
				a.tabs[i] = updatedTab
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}

	return a, tea.Batch(cmds...)
}

func (a *App) View() string {
	if a.quitting {
		return "Goodbye!\n"
	}

	header := a.header.View()
	tabContent := a.tabs[a.activeTab].View()
	content := styles.ContentStyle.Render(tabContent)
	status := a.status.View()
	help := a.help.View()

	return lipgloss.JoinVertical(lipgloss.Top, header, content, status, help)
}

func (a *App) tick() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg {
		return messages.TickMsg{}
	})
}

func (a *App) clearStatusBarTick() tea.Cmd {
	return tea.Tick(statusBarTickInterval, func(time.Time) tea.Msg {
		return messages.ClearStatusBarTickMsg{}
	})
}

func (a *App) clearStatus() tea.Cmd {
	return func() tea.Msg {
		return messages.ClearStatusBarMsg{}
	}
}

func (a *App) fetchStatus() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		resp, err := a.client.Statistics(ctx, "")
		if err != nil {
			return messages.StatusUpdateMsg{Err: err}
		}

		if !resp.Success {
			return messages.StatusUpdateMsg{Err: fmt.Errorf("%s", resp.Error)}
		}

		// Parse statistics from response
		stats, err := parseStatistics(resp.Data)
		if err != nil {
			return messages.StatusUpdateMsg{Err: err}
		}

		return messages.StatusUpdateMsg{Statistics: stats}
	}
}

// parseStatistics converts the response data to Statistics map.
func parseStatistics(data any) (map[string]meeseeks.Statistics, error) {
	// The data comes as map[string]interface{} from JSON
	dataMap, isMap := data.(map[string]any)
	if !isMap {
		return nil, errors.New("unexpected data format")
	}

	stats := make(map[string]meeseeks.Statistics)
	for name, v := range dataMap {
		progMap, isProg := v.(map[string]any)
		if !isProg {
			continue
		}

		stat := meeseeks.Statistics{
			ProgramName: getString(progMap, "program_name"),
			State:       getString(progMap, "state"),
			Successful:  getInt(progMap, "successful_runs"),
			Failed:      getInt(progMap, "failed_runs"),
			Retries:     getInt(progMap, "retries"),
			Stdout:      getString(progMap, "stdout"),
			Stderr:      getString(progMap, "stderr"),
			Interval:    getString(progMap, "interval"),
			LastRunAt:   getString(progMap, "last_run_at"),
			NextRunAt:   getString(progMap, "next_run"),
		}
		stats[name] = stat
	}

	return stats, nil
}

func getString(m map[string]any, key string) string {
	v, exists := m[key]
	if !exists {
		return ""
	}
	s, isString := v.(string)
	if !isString {
		return ""
	}
	return s
}

func getInt(m map[string]any, key string) int {
	v, exists := m[key]
	if !exists {
		return 0
	}
	f, isFloat := v.(float64)
	if !isFloat {
		return 0
	}
	return int(f)
}
