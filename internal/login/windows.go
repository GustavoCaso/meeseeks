//go:build windows

package login

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"unicode/utf16"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

// windowsService implements Service for Windows using Task Scheduler.
type windowsService struct {
	logger *logger.Logger
}

// getPlatformService returns the Windows-specific login service implementation.
func getPlatformService(logger *logger.Logger) Service {
	return &windowsService{
		logger: logger,
	}
}

const taskName = "meeseeks"

// taskXMLTemplate defines a Task Scheduler task that starts meeseeks at user
// logon and restarts it on failure (KeepAlive parity with the macOS agent).
const taskXMLTemplate = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>meeseeks process manager</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
    </LogonTrigger>
  </Triggers>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <Enabled>true</Enabled>
    <Hidden>true</Hidden>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
  </Settings>
  <Actions>
    <Exec>
      <Command>{{.ExecutablePath}}</Command>
      <Arguments>start -config "{{.ConfigPath}}"</Arguments>
    </Exec>
  </Actions>
</Task>
`

func (d *windowsService) Create(ctx context.Context, config ServiceConfig) (Defintion, error) {
	taskPath := getTaskXMLPath()

	// Check if the scheduled task already exists.
	//nolint:gosec // the task name is controlled by us
	query := exec.CommandContext(ctx, "schtasks", "/query", "/tn", taskName)
	if err := query.Run(); err == nil {
		return "", fmt.Errorf("service already exists: scheduled task %q", taskName)
	} else if _, ok := err.(*exec.ExitError); !ok {
		return "", fmt.Errorf("failed to query scheduled task: %w", err)
	}

	// Also guard against a leftover task XML file.
	if _, err := os.Stat(taskPath); err == nil {
		return "", fmt.Errorf("service already exists at %s", taskPath)
	}

	// Ensure the task XML directory exists.
	if err := os.MkdirAll(filepath.Dir(taskPath), 0750); err != nil {
		return "", fmt.Errorf("failed to create task directory: %w", err)
	}

	// Ensure config directory exists.
	if err := os.MkdirAll(config.ConfigDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	tmpl, err := template.New("task").Parse(taskXMLTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse task template: %w", err)
	}

	var rendered bytes.Buffer
	if execErr := tmpl.Execute(&rendered, config); execErr != nil {
		return "", fmt.Errorf("failed to execute task template: %w", execErr)
	}

	// schtasks /xml requires a UTF-16LE encoded file with a BOM.
	if err := os.WriteFile(taskPath, encodeUTF16LEWithBOM(rendered.String()), 0600); err != nil {
		return "", fmt.Errorf("failed to write task file: %w", err)
	}

	return Defintion(taskPath), nil
}

// Enable registers the scheduled task so meeseeks starts at user logon.
func (d *windowsService) Enable(ctx context.Context, service Defintion) error {
	//nolint:gosec // the arguments are controlled by us
	cmd := exec.CommandContext(
		ctx,
		"schtasks",
		"/create",
		"/tn",
		taskName,
		"/xml",
		string(service),
		"/f",
	)
	if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		return fmt.Errorf(
			"failed to create scheduled task: %s, output: %s",
			cmdErr.Error(),
			string(output),
		)
	}

	return nil
}

// Disable removes the scheduled task and its XML definition.
func (d *windowsService) Disable(ctx context.Context) error {
	taskPath := getTaskXMLPath()

	//nolint:gosec // the task name is controlled by us
	cmd := exec.CommandContext(ctx, "schtasks", "/delete", "/tn", taskName, "/f")
	if output, err := cmd.CombinedOutput(); err != nil {
		d.logger.Warn(
			"Error deleting the scheduled task",
			"error",
			err.Error(),
			"message",
			string(output),
		)
	}

	if err := os.Remove(taskPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove task file: %w", err)
	}

	return nil
}

// Status returns the current status of the scheduled task on Windows.
func (d *windowsService) Status(ctx context.Context) (ServiceStatus, error) {
	status := ServiceStatus{}

	//nolint:gosec // the task name is controlled by us
	query := exec.CommandContext(ctx, "schtasks", "/query", "/tn", taskName, "/fo", "LIST", "/v")
	output, err := query.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// A non-zero exit means the task does not exist.
			status.Enabled = false
			return status, nil
		}
		return status, fmt.Errorf("failed to query scheduled task: %w", err)
	}

	status.Enabled = true

	if strings.Contains(string(output), "Status:") &&
		strings.Contains(string(output), "Running") {
		status.Running = true
	} else {
		return status, nil
	}

	// Try to get last run time from log file.
	logPath := getLogPath("out")
	if stat, statErr := os.Stat(logPath); statErr == nil {
		status.LastRun = stat.ModTime()
	}

	// Check for errors in error log.
	errorLogPath := getLogPath("error")
	if errorData, readErr := os.ReadFile(errorLogPath); readErr == nil && len(errorData) > 0 {
		status.Error = string(errorData)
	}

	return status, nil
}

// getTaskXMLPath returns the path to the generated Task Scheduler XML file.
func getTaskXMLPath() string {
	testDir, ok := os.LookupEnv("MEESEEKS_TEST_LOGIN_DIR")
	if ok {
		return filepath.Join(testDir, "meeseeks-task.xml")
	}

	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		homeDir, _ := os.UserHomeDir()
		base = homeDir
	}
	return filepath.Join(base, "meeseeks", "meeseeks-task.xml")
}

// encodeUTF16LEWithBOM encodes s as UTF-16LE prefixed with a byte-order mark,
// the format schtasks expects for /xml input.
func encodeUTF16LEWithBOM(s string) []byte {
	u16 := utf16.Encode([]rune(s))
	buf := make([]byte, 0, 2+len(u16)*2)
	buf = append(buf, 0xFF, 0xFE) // UTF-16LE BOM
	for _, r := range u16 {
		buf = binary.LittleEndian.AppendUint16(buf, r)
	}
	return buf
}
