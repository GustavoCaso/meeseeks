package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/internal/login"
)

func runAtLoginCommand(args []string, logger *logger.Logger) error {
	if len(args) < 1 {
		printRunAtLoginUsage()
		return errors.New("subcommand required")
	}

	// Handle help flag at the top level
	if args[0] == "-h" || args[0] == "--help" {
		printRunAtLoginUsage()
		return nil
	}

	subcommand := args[0]
	subcommandArgs := args[1:]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Get platform-specific login service
	service := login.GetService(logger)

	switch subcommand {
	case "enable":
		return runAtLoginEnableCommand(ctx, service, subcommandArgs)
	case "disable":
		return runAtLoginDisableCommand(ctx, service, subcommandArgs)
	case "status":
		return runAtLoginStatusCommand(ctx, service, subcommandArgs)
	default:
		printRunAtLoginUsage()
		return fmt.Errorf("unknown subcommand: %s", subcommand)
	}
}

func runAtLoginEnableCommand(ctx context.Context, service login.Service, args []string) error {
	fs := flag.NewFlagSet("run-at-login enable", flag.ExitOnError)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks run-at-login enable\n\n")
		fmt.Fprintf(os.Stderr, "Configure meeseeks to start automatically at login\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	configDir := getMeeseeksDir()

	// Convert to absolute path if not already
	absConfigDir, err := filepath.Abs(configDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for config dir: %w", err)
	}

	// Convert to absolute path if not already
	absConfigPath, err := filepath.Abs(getDefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to get absolute path for config file: %w", err)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	absExecPath, absErr := filepath.Abs(execPath)
	if absErr != nil {
		return fmt.Errorf("failed to get absolute path for executable: %w", absErr)
	}

	// Create login service configuration
	loginConfig := login.ServiceConfig{
		ConfigPath:     absConfigPath,
		ExecutablePath: absExecPath,
		ConfigDir:      absConfigDir,
	}

	serviceDefnition, createErr := service.Create(ctx, loginConfig)

	if createErr != nil {
		return fmt.Errorf("failed to create login service: %w", createErr)
	}

	// Enable the service
	if enableErr := service.Enable(ctx, serviceDefnition); enableErr != nil {
		return fmt.Errorf("failed to enable login service: %w", enableErr)
	}

	fmt.Fprintf(os.Stdout, "Successfully enabled meeseeks to start at login\n")
	fmt.Fprintf(os.Stdout, "Config file: %s\n", absConfigPath)

	return nil
}

func runAtLoginDisableCommand(ctx context.Context, service login.Service, args []string) error {
	fs := flag.NewFlagSet("run-at-login disable", flag.ExitOnError)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks run-at-login disable\n\n")
		fmt.Fprintf(os.Stderr, "Remove automatic startup configuration\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Disable the service
	if err := service.Disable(ctx); err != nil {
		return fmt.Errorf("failed to disable login service: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Successfully disabled meeseeks login service\n")

	return nil
}

func runAtLoginStatusCommand(ctx context.Context, service login.Service, args []string) error {
	fs := flag.NewFlagSet("run-at-login status", flag.ExitOnError)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks run-at-login status\n\n")
		fmt.Fprintf(os.Stderr, "Show current login service status\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Get service status
	status, err := service.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get login service status: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Enabled: %t\n", status.Enabled)
	fmt.Fprintf(os.Stdout, "Running: %t\n", status.Running)

	if !status.LastRun.IsZero() {
		fmt.Fprintf(os.Stdout, "Last Run: %s\n", status.LastRun.Format("2006-01-02 15:04:05"))
	}

	if status.Error != "" {
		fmt.Fprintf(os.Stdout, "Error: %s\n", status.Error)
	}

	return nil
}

func printRunAtLoginUsage() {
	fmt.Fprintf(os.Stderr, "Usage: meeseeks run-at-login <subcommand>\n\n")
	fmt.Fprintf(os.Stderr, "Manage automatic startup of meeseeks at user login\n\n")
	fmt.Fprintf(os.Stderr, "Subcommands:\n")
	fmt.Fprintf(os.Stderr, "  enable   Configure meeseeks to start automatically at login\n")
	fmt.Fprintf(os.Stderr, "  disable  Remove automatic startup configuration\n")
	fmt.Fprintf(os.Stderr, "  status   Show current login service status\n\n")
	fmt.Fprintf(os.Stderr, "Use 'meeseeks run-at-login <subcommand> -h' for more information about a subcommand.\n")
}
