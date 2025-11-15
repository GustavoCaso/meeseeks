//go:build linux

package login

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

// linuxService implements Service for Linux using systemd user services.
type linuxService struct {
	logger *logger.Logger
}

// getPlatformService returns the Linux-specific login service implementation.
func getPlatformService(logger *logger.Logger) Service {
	return &linuxService{
		logger: logger,
	}
}

const serviceName = "meeseeks.service"

const systemdUnitTemplate = `[Unit]
Description=Meeseeks Service
Documentation=https://github.com/GustavoCaso/meeseeks
After=default.target

[Service]
Type=simple
ExecStart={{.ExecutablePath}} start -config {{.ConfigPath}}
Restart=on-failure
RestartSec=10
StandardOutput=append:{{.ConfigDir}}/meeseeks.out.log
StandardError=append:{{.ConfigDir}}/meeseeks.error.log
Environment="MEESEEKS_CONFIG_DIR={{.ConfigDir}}"

[Install]
WantedBy=default.target
`

// Create generates a systemd unit file for the meeseeks service.
func (l *linuxService) Create(ctx context.Context, config ServiceConfig) (Defintion, error) {
	unitPath := getSystemdUnitPath()

	// Check if service already exists
	if _, err := os.Stat(unitPath); err == nil {
		return "", fmt.Errorf("service already exists at %s", unitPath)
	}

	// Ensure systemd user directory exists
	systemdUserDir := filepath.Dir(unitPath)
	if err := os.MkdirAll(systemdUserDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create systemd user directory: %w", err)
	}

	// Ensure config directory exists
	if err := os.MkdirAll(config.ConfigDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create unit file from template
	tmpl, err := template.New("systemd-unit").Parse(systemdUnitTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse systemd unit template: %w", err)
	}

	file, err := os.Create(unitPath)
	if err != nil {
		return "", fmt.Errorf("failed to create systemd unit file: %w", err)
	}
	defer file.Close()

	if execErr := tmpl.Execute(file, config); execErr != nil {
		return "", fmt.Errorf("failed to execute systemd unit template: %w", execErr)
	}

	// Set appropriate permissions
	if chmodErr := os.Chmod(unitPath, 0644); chmodErr != nil {
		return "", fmt.Errorf("failed to set systemd unit file permissions: %w", chmodErr)
	}

	// Reload systemd daemon to recognize the new unit file
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload")
	if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		return "", fmt.Errorf("failed to reload systemd daemon: %s, output: %s", cmdErr.Error(), string(output))
	}

	return Defintion(unitPath), nil
}

// Enable configures meeseeks to start automatically at user login.
func (l *linuxService) Enable(ctx context.Context, service Defintion) error {
	// Enable the service to start at login
	enableCmd := exec.CommandContext(ctx, "systemctl", "--user", "enable", serviceName)
	if output, err := enableCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable service: %s, output: %s", err.Error(), string(output))
	}

	// Start the service immediately
	startCmd := exec.CommandContext(ctx, "systemctl", "--user", "start", serviceName)
	if output, err := startCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start service: %s, output: %s", err.Error(), string(output))
	}

	return nil
}

// Disable removes the automatic startup configuration and stops the service.
func (l *linuxService) Disable(ctx context.Context) error {
	unitPath := getSystemdUnitPath()

	// Check if service exists
	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		return fmt.Errorf("service %s not found", unitPath)
	}

	// Stop the service if it's running
	stopCmd := exec.CommandContext(ctx, "systemctl", "--user", "stop", serviceName)
	output, err := stopCmd.CombinedOutput()
	if err != nil {
		l.logger.Warn("Error stopping the service", "error", err.Error(), "message", string(output))
	}

	// Disable the service
	disableCmd := exec.CommandContext(ctx, "systemctl", "--user", "disable", serviceName)
	output, err = disableCmd.CombinedOutput()
	if err != nil {
		l.logger.Warn("Error disabling the service", "error", err.Error(), "message", string(output))
	}

	// Remove the unit file
	if err = os.Remove(unitPath); err != nil {
		return fmt.Errorf("failed to remove systemd unit file: %w", err)
	}

	// Reload systemd daemon
	reloadCmd := exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload")
	if output, reloadErr := reloadCmd.CombinedOutput(); reloadErr != nil {
		l.logger.Warn("Error reloading systemd daemon", "error", reloadErr.Error(), "message", string(output))
	}

	return nil
}

// Status returns the current status of the login service.
func (l *linuxService) Status(ctx context.Context) (ServiceStatus, error) {
	status := ServiceStatus{}
	unitPath := getSystemdUnitPath()

	// Check if unit file exists
	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		status.Enabled = false
		return status, nil
	}

	// Check if service is enabled
	isEnabledCmd := exec.CommandContext(ctx, "systemctl", "--user", "is-enabled", serviceName)
	output, err := isEnabledCmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	// is-enabled returns "enabled", "disabled", or exits with error
	if err == nil && outputStr == "enabled" {
		status.Enabled = true
	} else {
		status.Enabled = false
		return status, nil
	}

	// Check if service is running
	isActiveCmd := exec.CommandContext(ctx, "systemctl", "--user", "is-active", serviceName)
	activeOutput, activeErr := isActiveCmd.CombinedOutput()
	activeOutputStr := strings.TrimSpace(string(activeOutput))

	// is-active returns "active", "inactive", "failed", etc.
	if activeErr == nil && activeOutputStr == "active" {
		status.Running = true
	}

	// Try to get last run time from log file
	logPath := getLogPath("out")
	if stat, statErr := os.Stat(logPath); statErr == nil {
		status.LastRun = stat.ModTime()
	}

	// Check for errors in error log
	errorLogPath := getLogPath("error")
	if errorData, readErr := os.ReadFile(errorLogPath); readErr == nil && len(errorData) > 0 {
		status.Error = string(errorData)
	}

	return status, nil
}

// getSystemdUnitPath returns the path to the systemd user unit file.
func getSystemdUnitPath() string {
	testDir, ok := os.LookupEnv("MEESEEKS_TEST_LOGIN_DIR")
	if ok {
		return filepath.Join(testDir, serviceName)
	}

	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "systemd", "user", serviceName)
}

// getLogPath returns the path to the log file for the service.
func getLogPath(logType string) string {
	homeDir, _ := os.UserHomeDir()
	meseeksDir := os.Getenv("MEESEEKS_CONFIG_DIR")
	if meseeksDir == "" {
		meseeksDir = filepath.Join(homeDir, ".meeseeks")
	}
	return filepath.Join(meseeksDir, fmt.Sprintf("meeseeks.%s.log", logType))
}
