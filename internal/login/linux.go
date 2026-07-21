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

// linuxService implements Service for Linux using a systemd user service.
type linuxService struct {
	logger *logger.Logger
}

// getPlatformService returns the Linux-specific login service implementation.
func getPlatformService(logger *logger.Logger) Service {
	return &linuxService{
		logger: logger,
	}
}

const unitName = "meeseeks.service"

const systemdUnitTemplate = `[Unit]
Description=meeseeks process manager

[Service]
Type=simple
ExecStart="{{.ExecutablePath}}" start -config "{{.ConfigPath}}"
Restart=always
Environment="MEESEEKS_CONFIG_DIR={{.ConfigDir}}"
StandardOutput=append:"{{.ConfigDir}}/meeseeks.out.log"
StandardError=append:"{{.ConfigDir}}/meeseeks.error.log"

[Install]
WantedBy=multi-user.target
`

// ensureSystemd returns a clear error if systemd is not available on the host.
func ensureSystemd() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf(
			"systemd is required for start-at-login on Linux (systemctl not found): %w",
			err,
		)
	}
	return nil
}

func (d *linuxService) Create(_ context.Context, config ServiceConfig) (Defintion, error) {
	unitPath, unitPathErr := getUnitPath()

	if unitPathErr != nil {
		return "", fmt.Errorf("fail to get user unitPath %w", unitPathErr)
	}

	// Check if service already exists
	if _, err := os.Stat(unitPath); err == nil {
		return "", fmt.Errorf("service already exists at %s", unitPath)
	}

	// Ensure the systemd user unit directory exists
	if err := os.MkdirAll(filepath.Dir(unitPath), 0750); err != nil {
		return "", fmt.Errorf("failed to create systemd user directory: %w", err)
	}

	// Ensure config directory exists
	if err := os.MkdirAll(config.ConfigDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	tmpl, err := template.New("unit").Parse(systemdUnitTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse unit template: %w", err)
	}

	file, err := os.Create(unitPath)
	if err != nil {
		return "", fmt.Errorf("failed to create unit file: %w", err)
	}
	defer file.Close()

	if execErr := tmpl.Execute(file, config); execErr != nil {
		return "", fmt.Errorf("failed to execute unit template: %w", execErr)
	}

	if chmodErr := os.Chmod(unitPath, 0600); chmodErr != nil {
		return "", fmt.Errorf("failed to set unit file permissions: %w", chmodErr)
	}

	return Defintion(unitPath), nil
}

// Enable configures meeseeks to start automatically at user login on Linux.
func (d *linuxService) Enable(ctx context.Context, _ Defintion) error {
	if err := ensureSystemd(); err != nil {
		return err
	}

	reload := exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload")
	if output, cmdErr := reload.CombinedOutput(); cmdErr != nil {
		return fmt.Errorf(
			"failed to reload systemd user daemon: %s, output: %s",
			cmdErr.Error(),
			string(output),
		)
	}

	cmd := exec.CommandContext(ctx, "systemctl", "--user", "enable", "--now", unitName)
	if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		return fmt.Errorf(
			"failed to enable service with systemctl: %s, output: %s",
			cmdErr.Error(),
			string(output),
		)
	}

	return nil
}

// Disable removes the automatic startup configuration for Linux.
func (d *linuxService) Disable(ctx context.Context) error {
	if err := ensureSystemd(); err != nil {
		return err
	}

	unitPath, unitPathErr := getUnitPath()

	if unitPathErr != nil {
		return fmt.Errorf("failed to get unit path: %w", unitPathErr)
	}

	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		return fmt.Errorf("service %s not found", unitPath)
	}

	cmd := exec.CommandContext(ctx, "systemctl", "--user", "disable", "--now", unitName)
	if output, err := cmd.CombinedOutput(); err != nil {
		d.logger.Warn(
			"Error disabling the service",
			"error",
			err.Error(),
			"message",
			string(output),
		)
	}

	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("failed to remove unit file: %w", err)
	}

	// Reload so systemd forgets the removed unit.
	reload := exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload")
	if output, err := reload.CombinedOutput(); err != nil {
		d.logger.Warn(
			"Error reloading systemd user daemon",
			"error",
			err.Error(),
			"message",
			string(output),
		)
	}

	return nil
}

// Status returns the current status of the login service on Linux.
func (d *linuxService) Status(ctx context.Context) (ServiceStatus, error) {
	status := ServiceStatus{}

	if err := ensureSystemd(); err != nil {
		return status, err
	}

	unitPath, unitPathErr := getUnitPath()
	if unitPathErr != nil {
		return status, fmt.Errorf("failed to get unit path: %w", unitPathErr)
	}

	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		status.Enabled = false
		return status, nil
	}

	status.Enabled = true

	// systemctl is-active exits non-zero when not active; that is not a failure
	// for us, we only care about the reported state.
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "is-active", unitName)
	output, cmdErr := cmd.CombinedOutput()
	if cmdErr != nil {
		return status, fmt.Errorf(
			"failed to query systemd service status: %s",
			cmdErr.Error(),
		)
	}
	if strings.TrimSpace(string(output)) == "active" {
		status.Running = true
	} else {
		return status, nil
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

// getUnitPath returns the path to the systemd user unit file.
func getUnitPath() (string, error) {
	testDir, ok := os.LookupEnv("MEESEEKS_TEST_LOGIN_DIR")
	if ok {
		return filepath.Join(testDir, unitName), nil
	}

	userConfig, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(userConfig, "systemd", "user", unitName), nil
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
