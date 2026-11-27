package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartCommand_ConfigValidation(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	emptyConfigDir := filepath.Join(tmpDir, "nonexistent")

	tests := []struct {
		name         string
		configDir    string
		expectedExit int
		errorMessage string
	}{
		{
			name:         "nonexistent config directory",
			configDir:    emptyConfigDir,
			expectedExit: 1,
			errorMessage: "failed to load config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdoutBuf, stderrBuf bytes.Buffer
			exitCode := runCLICommand(
				[]string{"start"},
				&stdoutBuf,
				&stderrBuf,
				5*time.Second,
				fmt.Sprintf("MEESEEKS_CONFIG_DIR=%s", tt.configDir),
			)
			output := stdoutBuf.String() + stderrBuf.String()

			if exitCode != tt.expectedExit {
				t.Fatalf("Expected exit code %d, got %d", tt.expectedExit, exitCode)
			}

			if !strings.Contains(output, tt.errorMessage) {
				t.Fatalf("Expected error message %q, got %q", tt.errorMessage, output)
			}
		})
	}
}

func TestStartCommand_Help(t *testing.T) {
	t.Parallel()
	testCommandHelp(t, []string{"start"}, []string{
		"Usage: meeseeks start [options]",
		"Start programs from configuration file",
		"-d",
	})
}

func TestStartCommand_Detached(t *testing.T) {
	configDir := setMeeseeksConfigDirForTest(t)

	configFile := filepath.Join(configDir, "config.yaml")
	configContent := `programs:
  - name: "test-echo-detached"
    command: "sleep"
    args: ["30"]
`

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	expectedPidFile := getPidFile()
	expectedSocketPath := getSocketPath()

	os.Remove(expectedPidFile)
	os.Remove(expectedSocketPath)

	var stdout, stderr bytes.Buffer
	exitCode := runCLICommand(
		[]string{"start", "-d"},
		&stdout,
		&stderr,
		15*time.Second,
	)

	if exitCode != 0 {
		t.Fatalf(
			"Failed to start daemon: exit code %d\nStdout: %s\nStderr: %s",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}

	output := stdout.String() + stderr.String()
	if !strings.Contains(output, "Started meeseeks (detached)") {
		t.Fatalf("Expected `Started meeseeks (detached)`, got: %q", output)
	}

	if _, err := os.Stat(expectedPidFile); os.IsNotExist(err) {
		t.Fatalf("PID file was not created at %s", expectedPidFile)
	}

	waitFor(t, 5*time.Second, func() bool {
		_, err := os.Stat(expectedSocketPath)
		return err == nil
	}, "Socket file was not created at "+expectedSocketPath)

	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode = runCLICommand(
		[]string{"status"},
		&stdoutBuf,
		&stderrBuf,
		5*time.Second,
	)
	if exitCode != 0 {
		t.Fatalf("Expected exit code %d, got %d", 0, exitCode)
	}
	statusOutput := stdoutBuf.String() + stderrBuf.String()

	if strings.Contains(statusOutput, "meeseeks server not running") {
		t.Fatalf("Status command could not connect to daemon: %q", statusOutput)
	}

	exitCode = runCLICommand([]string{"exit"}, nil, nil, 5*time.Second)
	if exitCode != 0 {
		t.Fatalf("Expected exit code %d, got %d", 0, exitCode)
	}

	// Wait for process cleanup to complete after exit signal
	waitFor(t, 5*time.Second, func() bool {
		_, err := os.Stat(expectedPidFile)
		return os.IsNotExist(err)
	}, "PID file still exists. Stoping meeseeks should remove the PID file")

	waitFor(t, 5*time.Second, func() bool {
		_, err := os.Stat(expectedSocketPath)
		return os.IsNotExist(err)
	}, "Socket file still exists. Stoping meeseeks should remove the Socket file")

	var stdoutBuf2, stderrBuf2 bytes.Buffer
	exitCode = runCLICommand(
		[]string{"status"},
		&stdoutBuf2,
		&stderrBuf2,
		5*time.Second,
	)
	if exitCode != 1 {
		t.Fatalf("Expected exit code %d, got %d", 1, exitCode)
	}
	statusOutput = stdoutBuf2.String() + stderrBuf2.String()

	if !strings.Contains(statusOutput, "meeseeks server not running") {
		t.Fatal("Status command should not work after exiting meeseeks")
	}
}
