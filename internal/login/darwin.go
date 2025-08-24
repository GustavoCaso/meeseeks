//go:build darwin

package login

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

// darwinService implements LoginService for macOS using LaunchAgent.
type darwinService struct {
	logger *logger.Logger
}

// getPlatformService returns the macOS-specific login service implementation.
func getPlatformService(logger *logger.Logger) Service {
	return &darwinService{
		logger: logger,
	}
}

const label = "com.meeseeks"

const launchAgentTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.meeseeks</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.ExecutablePath}}</string>
        <string>run</string>
        <string>-config</string>
        <string>{{.ConfigPath}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardErrorPath</key>
    <string>{{.ConfigDir}}/meeseeks.error.log</string>
    <key>StandardOutPath</key>
    <string>{{.ConfigDir}}/meeseeks.out.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>MEESEEKS_CONFIG_DIR</key>
        <string>{{.ConfigDir}}</string>
    </dict>
</dict>
</plist>`

func (d *darwinService) Create(config ServiceConfig) (Defintion, error) {
	plistPath := getLaunchAgentPath()

	// Check if service already exists
	if _, err := os.Stat(plistPath); err == nil {
		return "", fmt.Errorf("service already exists at %s", plistPath)
	}

	// Ensure LaunchAgents directory exists
	launchAgentDir := filepath.Dir(plistPath)
	if err := os.MkdirAll(launchAgentDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create LaunchAgent directory: %w", err)
	}

	// Ensure config directory exists
	if err := os.MkdirAll(config.ConfigDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create plist file from template
	tmpl, err := template.New("plist").Parse(launchAgentTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse plist template: %w", err)
	}

	file, err := os.Create(plistPath)
	if err != nil {
		return "", fmt.Errorf("failed to create plist file: %w", err)
	}
	defer file.Close()

	if execErr := tmpl.Execute(file, config); execErr != nil {
		return "", fmt.Errorf("failed to execute plist template: %w", execErr)
	}

	// Set appropriate permissions
	if chmodErr := os.Chmod(plistPath, 0600); chmodErr != nil {
		return "", fmt.Errorf("failed to set plist file permissions: %w", chmodErr)
	}

	validationCmd := exec.Command("plutil", "-lint", plistPath)
	output, validationCmdErr := validationCmd.CombinedOutput()
	if validationCmdErr != nil {
		return "", fmt.Errorf(
			"failed to validate service with plutil: %s, output: %s",
			validationCmdErr.Error(),
			string(output),
		)
	}
	okStatus := fmt.Sprintf("%s: OK\n", plistPath)

	if string(output) != okStatus {
		return "", fmt.Errorf("invalid plist service defintion: %s, output: %s", plistPath, string(output))
	}

	return Defintion(plistPath), nil
}

// Enable configures meeseeks to start automatically at user login on macOS.
func (d *darwinService) Enable(service Defintion) error {
	// Load the service using launchctl
	//nolint:gosec // the arguments are provided by the user
	cmd := exec.Command("launchctl", "load", string(service))
	if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		return fmt.Errorf("failed to load service with launchctl: %s, output: %s", cmdErr.Error(), string(output))
	}

	return nil
}

// Disable removes the automatic startup configuration for macOS.
func (d *darwinService) Disable() error {
	plistPath := getLaunchAgentPath()

	// Check if service exists
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return fmt.Errorf("service %s not found", plistPath)
	}

	userID, err := exec.Command("id", "-u").CombinedOutput()
	if err != nil {
		return fmt.Errorf("fail to get user id: %w", err)
	}

	iD := strings.TrimSpace(string(userID))

	//nolint:gosec // the arguments are provided by the user
	cmd := exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%s", iD), plistPath)
	output, err := cmd.CombinedOutput()

	if err != nil {
		d.logger.Warn("Error unloading the service", "error", err.Error(), "message", string(output))
	}

	// Remove the plist file
	if err = os.Remove(plistPath); err != nil {
		return fmt.Errorf("failed to remove plist file: %w", err)
	}

	return nil
}

// Status returns the current status of the login service on macOS.
func (d *darwinService) Status() (ServiceStatus, error) {
	status := ServiceStatus{}
	plistPath := getLaunchAgentPath()

	// Check if plist file exists
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		status.Enabled = false
		return status, nil
	}

	status.Enabled = true

	userID, err := exec.Command("id", "-u").CombinedOutput()
	if err != nil {
		return status, fmt.Errorf("fail to get user id: %w", err)
	}

	iD := strings.TrimSpace(string(userID))

	// Check if service is running using launchctl list
	//nolint:gosec // the arguments are controlled by us
	cmd := exec.Command("launchctl", "print-disabled", fmt.Sprintf("gui/%s", iD))
	output, err := cmd.CombinedOutput()

	if err != nil {
		return status, fmt.Errorf("fail launchctl print-disabled: %w", err)
	}

	expectedString := fmt.Sprintf("\"%s\" => enabled", label)
	// Service is loaded, check if it's running
	outputStr := string(output)

	// If the label does not show or the it shows as enabled the service is running
	if !strings.Contains(outputStr, label) || strings.Contains(outputStr, expectedString) {
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

// getLaunchAgentPath returns the path to the LaunchAgent plist file.
func getLaunchAgentPath() string {
	testDir, ok := os.LookupEnv("MEESEEKS_TEST_LOGIN_DIR")
	if ok {
		return filepath.Join(testDir, "com.meeseeks.plist")
	}

	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, "Library", "LaunchAgents", "com.meeseeks.plist")
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
