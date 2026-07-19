package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/internal/server"
	"github.com/GustavoCaso/meeseeks/internal/tui"
	"github.com/GustavoCaso/meeseeks/internal/tui/tab"
)

func tuiCommand(args []string, _ *logger.Logger) error {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks tui\n\n")
		fmt.Fprintf(os.Stderr, "Launch interactive terminal UI\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	socketPath := getSocketPath()
	configPath := getDefaultConfigPath()

	// Check if daemon is running
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		return errors.New("daemon not running: run 'meeseeks start -d' first")
	}

	// Create client
	client := server.NewClient(socketPath)

	// Create tabs
	tabList := []tab.Tab{
		tab.NewPrograms(client),
		tab.NewConfig(client, configPath),
	}

	// Create and run app
	app := tui.NewApp(client, configPath, tabList, version)
	p := tea.NewProgram(app, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	return nil
}
