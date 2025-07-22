package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/config"
)

// Helper function to run CLI commands as subprocess.
func runCLICommand(
	t *testing.T,
	args []string,
	stdoutBuf io.Writer,
	stderrBuf io.Writer,
	timeout time.Duration,
) int {
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "main.go")
	cmd.Args = append(cmd.Args, args...)
	cmd.Dir = "/Users/gustavocaso/src/github.com/GustavoCaso/meeseeks/cmd/meeseeks"

	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		} else {
			// Context timeout or other error
			exitCode = 1
		}
	}

	return exitCode
}

func TestMain(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "no command",
			test: func(t *testing.T) {
				var stdoutBuf, stderrBuf bytes.Buffer
				exitCode := runCLICommand(t, []string{}, &stdoutBuf, &stderrBuf, 5*time.Second)
				output := stdoutBuf.String() + stderrBuf.String()

				if exitCode != 1 {
					t.Errorf("Expected exit code 1, got %d", exitCode)
				}

				expectedMessages := []string{
					"Usage: meeseeks <command>",
					"run     Start programs from config file",
					"status  Show status of running programs",
					"logs    Show logs for a specific program",
					"stop    Stop running programs",
					"version Show version information",
				}

				for _, msg := range expectedMessages {
					if !strings.Contains(output, msg) {
						t.Errorf("Expected output to contain %q, got %q", msg, output)
					}
				}
			},
		},
		{
			name: "unknown command",
			test: func(t *testing.T) {
				var stdoutBuf, stderrBuf bytes.Buffer
				exitCode := runCLICommand(t, []string{"unknown"}, &stdoutBuf, &stderrBuf, 5*time.Second)
				output := stdoutBuf.String() + stderrBuf.String()

				if exitCode != 1 {
					t.Errorf("Expected exit code 1, got %d", exitCode)
				}

				if !strings.Contains(output, "Unknown command: unknown") {
					t.Errorf("Expected unknown command error, got %q", output)
				}
			},
		},
		{
			name: "version command",
			test: func(t *testing.T) {
				var stdoutBuf, stderrBuf bytes.Buffer
				exitCode := runCLICommand(t, []string{"version"}, &stdoutBuf, &stderrBuf, 5*time.Second)
				output := stdoutBuf.String() + stderrBuf.String()

				if exitCode != 0 {
					t.Errorf("Expected exit code %d, got %d", 0, exitCode)
				}

				if !strings.Contains(output, "meeseeks version 1.0.0") {
					t.Errorf("Expected output to contain %q, got %q", "meeseeks version 1.0.0", output)
				}
			},
		},
		{
			name: "run command (detached)",
			test: func(t *testing.T) {
				// Create a temporary config file
				tmpDir := t.TempDir()
				configFile := filepath.Join(tmpDir, "test-detached-config.yaml")

				configContent := `programs:
  - name: "test-echo-detached"
    command: "sleep"
    args: ["30"]
`

				if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
					t.Fatalf("Failed to create test config file: %v", err)
				}

				// Unix sockets have a path length limit (~104-108 characters depending on OS).
				// Using t.TempDir() creates very long paths that exceed this limit and cause
				// "bind: invalid argument" errors. We use /tmp directly with short names instead.
				testHome := "/tmp"
				originalHome := os.Getenv("HOME")
				os.Setenv("HOME", testHome)
				defer os.Setenv("HOME", originalHome)

				// Create .meeseeks directory in test home
				meeseeksDir := filepath.Join(testHome, ".meeseeks")
				if err := os.MkdirAll(meeseeksDir, 0755); err != nil {
					t.Fatalf("Failed to create .meeseeks directory: %v", err)
				}

				expectedPidFile := filepath.Join(meeseeksDir, "meeseeks.pid")
				expectedSocketPath := filepath.Join(meeseeksDir, "meeseeks.sock")

				// Clean up any existing files from previous tests
				os.Remove(expectedPidFile)
				os.Remove(expectedSocketPath)
				defer func() {
					os.Remove(expectedPidFile)
					os.Remove(expectedSocketPath)
				}()

				// Start daemon in detached mode
				var stdout, stderr bytes.Buffer
				exitCode := runCLICommand(
					t,
					[]string{"run", "-d", "-config", configFile},
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
				if !strings.Contains(output, "Started meeseeks daemon") {
					t.Errorf("Expected daemon start message, got: %q", output)
				}

				// Verify PID file was created
				if _, err := os.Stat(expectedPidFile); os.IsNotExist(err) {
					t.Errorf("PID file was not created at %s", expectedPidFile)
				}

				// Wait a moment for daemon to fully start and check socket
				time.Sleep(1 * time.Second)

				// Debug: check if socket file exists
				if _, err := os.Stat(expectedSocketPath); os.IsNotExist(err) {
					t.Errorf("Socket file was not created at %s", expectedSocketPath)
				}

				// Test status command works
				var stdoutBuf, stderrBuf bytes.Buffer
				exitCode = runCLICommand(
					t,
					[]string{"status"},
					&stdoutBuf,
					&stderrBuf,
					5*time.Second,
				)
				if exitCode != 0 {
					t.Errorf("Expected exit code %d, got %d", 0, exitCode)
				}
				statusOutput := stdoutBuf.String() + stderrBuf.String()

				if strings.Contains(statusOutput, "failed to send request") {
					t.Errorf("Status command could not connect to daemon: %q", statusOutput)
				}

				// Cleanup: Stop the daemon
				defer func() {
					exitCode := runCLICommand(t, []string{"exit"}, nil, nil, 5*time.Second)
					if exitCode != 0 {
						t.Errorf("Expected exit code %d, got %d", 0, exitCode)
					}
				}()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.test(t)
		})
	}
}

func TestRunCommand_ConfigValidation(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectedExit int
		errorMessage string
	}{
		{
			name:         "missing config flag",
			args:         []string{"run"},
			expectedExit: 1,
			errorMessage: "-config flag is required",
		},
		{
			name:         "nonexistent config file",
			args:         []string{"run", "-config", "/nonexistent/file.yaml"},
			expectedExit: 1,
			errorMessage: "failed to load config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdoutBuf, stderrBuf bytes.Buffer
			exitCode := runCLICommand(t, tt.args, &stdoutBuf, &stderrBuf, 5*time.Second)
			output := stdoutBuf.String() + stderrBuf.String()

			if exitCode != tt.expectedExit {
				t.Errorf("Expected exit code %d, got %d", tt.expectedExit, exitCode)
			}

			if !strings.Contains(output, tt.errorMessage) {
				t.Errorf("Expected error message %q, got %q", tt.errorMessage, output)
			}
		})
	}
}

func TestRunCommand_ValidConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `programs:
  - name: "test-echo"
    command: "echo"
    args: ["hello", "from", "test"]
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	tests := []struct {
		name           string
		args           []string
		expectedOutput []string
		timeout        time.Duration
		sendInterrupt  bool
	}{
		{
			name: "foreground run with interrupt",
			args: []string{"run", "-config", configFile},
			expectedOutput: []string{
				"Starting 1 programs in foreground mode",
			},
			timeout:       3 * time.Second,
			sendInterrupt: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), tt.timeout)
			defer cancel()

			cmd := exec.CommandContext(ctx, "go", "run", "main.go")
			cmd.Args = append(cmd.Args, tt.args...)
			cmd.Dir = "/Users/gustavocaso/src/github.com/GustavoCaso/meeseeks/cmd/meeseeks"

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			// Send interrupt signal after a short delay to simulate Ctrl+C
			if tt.sendInterrupt {
				go func() {
					time.Sleep(500 * time.Millisecond)
					if cmd.Process != nil {
						cmd.Process.Signal(os.Interrupt)
					}
				}()
			}

			err := cmd.Run()

			// For foreground mode, we expect interrupt signal or completion
			output := stdout.String() + stderr.String()

			for _, expected := range tt.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, got %q", expected, output)
				}
			}

			// Check that no severe error occurred (ignoring interrupt signals)
			if err != nil && !strings.Contains(err.Error(), "signal") &&
				!strings.Contains(err.Error(), "interrupt") &&
				!strings.Contains(err.Error(), "context deadline") {
				t.Errorf("Unexpected error (ignoring interrupt/timeout): %v", err)
			}
		})
	}
}
func TestRunCommand_Help(t *testing.T) {
	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runCLICommand(t, []string{"run", "-h"}, &stdoutBuf, &stderrBuf, 5*time.Second)
	output := stdoutBuf.String() + stderrBuf.String()

	if exitCode != 0 {
		t.Errorf("Expected exit code 0 for help, got %d", exitCode)
	}

	expectedMessages := []string{
		"Usage: meeseeks run [options]",
		"Start programs from configuration file",
		"-config",
		"-d",
	}

	for _, msg := range expectedMessages {
		if !strings.Contains(output, msg) {
			t.Errorf("Expected help output to contain %q, got %q", msg, output)
		}
	}
}

func TestStatusCommand_Validation(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectedExit int
		errorMessage string
	}{
		{
			name:         "status with no daemon running",
			args:         []string{"status"},
			expectedExit: 1,
			errorMessage: "failed to send request",
		},
		{
			name:         "status specific program with no daemon",
			args:         []string{"status", "test-program"},
			expectedExit: 1,
			errorMessage: "failed to send request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdoutBuf, stderrBuf bytes.Buffer
			exitCode := runCLICommand(t, tt.args, &stdoutBuf, &stderrBuf, 5*time.Second)
			output := stdoutBuf.String() + stderrBuf.String()

			if exitCode != tt.expectedExit {
				t.Errorf("Expected exit code %d, got %d", tt.expectedExit, exitCode)
			}

			if !strings.Contains(output, tt.errorMessage) {
				t.Errorf("Expected error message %q, got %q", tt.errorMessage, output)
			}
		})
	}
}

func TestStatusCommand_Help(t *testing.T) {
	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runCLICommand(t, []string{"status", "-h"}, &stdoutBuf, &stderrBuf, 5*time.Second)
	output := stdoutBuf.String() + stderrBuf.String()

	if exitCode != 0 {
		t.Errorf("Expected exit code 0 for help, got %d", exitCode)
	}

	expectedMessages := []string{
		"Usage: meeseeks status [program_name]",
		"Show status of running programs",
	}

	for _, msg := range expectedMessages {
		if !strings.Contains(output, msg) {
			t.Errorf("Expected help output to contain %q, got %q", msg, output)
		}
	}
}

func TestLogsCommand_Validation(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectedExit int
		errorMessage string
	}{
		{
			name:         "logs without program name",
			args:         []string{"logs"},
			expectedExit: 1,
			errorMessage: "program name required",
		},
		{
			name:         "logs with no daemon running",
			args:         []string{"logs", "test-program"},
			expectedExit: 1,
			errorMessage: "failed to send request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdoutBuf, stderrBuf bytes.Buffer
			exitCode := runCLICommand(t, tt.args, &stdoutBuf, &stderrBuf, 5*time.Second)
			output := stdoutBuf.String() + stderrBuf.String()

			if exitCode != tt.expectedExit {
				t.Errorf("Expected exit code %d, got %d", tt.expectedExit, exitCode)
			}

			if !strings.Contains(output, tt.errorMessage) {
				t.Errorf("Expected error message %q, got %q", tt.errorMessage, output)
			}
		})
	}
}

func TestLogsCommand_Help(t *testing.T) {
	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runCLICommand(t, []string{"logs", "-h"}, &stdoutBuf, &stderrBuf, 5*time.Second)
	output := stdoutBuf.String() + stderrBuf.String()

	if exitCode != 0 {
		t.Errorf("Expected exit code 0 for help, got %d", exitCode)
	}

	expectedMessages := []string{
		"Usage: meeseeks logs <program_name>",
		"Show logs for a specific program",
	}

	for _, msg := range expectedMessages {
		if !strings.Contains(output, msg) {
			t.Errorf("Expected help output to contain %q, got %q", msg, output)
		}
	}
}

func TestStopCommand_Validation(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectedExit int
		errorMessage string
	}{
		{
			name:         "stop with no daemon running",
			args:         []string{"stop"},
			expectedExit: 1,
			errorMessage: "failed to send request",
		},
		{
			name:         "stop specific program with no daemon",
			args:         []string{"stop", "test-program"},
			expectedExit: 1,
			errorMessage: "failed to send request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdoutBuf, stderrBuf bytes.Buffer
			exitCode := runCLICommand(t, tt.args, &stdoutBuf, &stderrBuf, 5*time.Second)
			output := stdoutBuf.String() + stderrBuf.String()

			if exitCode != tt.expectedExit {
				t.Errorf("Expected exit code %d, got %d", tt.expectedExit, exitCode)
			}

			if !strings.Contains(output, tt.errorMessage) {
				t.Errorf("Expected error message %q, got %q", tt.errorMessage, output)
			}
		})
	}
}

func TestStopCommand_Help(t *testing.T) {
	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runCLICommand(t, []string{"stop", "-h"}, &stdoutBuf, &stderrBuf, 5*time.Second)
	output := stdoutBuf.String() + stderrBuf.String()

	if exitCode != 0 {
		t.Errorf("Expected exit code 0 for help, got %d", exitCode)
	}

	expectedMessages := []string{
		"Usage: meeseeks stop [program_name]",
		"Stop running programs",
	}

	for _, msg := range expectedMessages {
		if !strings.Contains(output, msg) {
			t.Errorf("Expected help output to contain %q, got %q", msg, output)
		}
	}
}

func TestCreateProgramFromConfig(t *testing.T) {
	tests := []struct {
		name         string
		config       config.ProgramConfig
		expectedName string
		expectError  bool
		errorMessage string
	}{
		{
			name: "basic program",
			config: config.ProgramConfig{
				Name:    "test-basic",
				Command: "echo",
				Args:    []string{"hello"},
			},
			expectedName: "test-basic",
		},
		{
			name: "program with environment variables",
			config: config.ProgramConfig{
				Name:    "test-env",
				Command: "bash",
				Args:    []string{"-c", "echo $TEST_VAR"},
				Env:     []string{"TEST_VAR=test_value"},
			},
			expectedName: "test-env",
		},
		{
			name: "program with interval",
			config: config.ProgramConfig{
				Name:     "test-interval",
				Command:  "echo",
				Interval: "30s",
			},
			expectedName: "test-interval",
		},
		{
			name: "program with invalid interval",
			config: config.ProgramConfig{
				Name:     "test-invalid",
				Command:  "echo",
				Interval: "invalid-duration",
			},
			expectError:  true,
			errorMessage: "invalid interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := createProgramFromConfig(tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errorMessage) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMessage, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if prog.Name() != tt.expectedName {
				t.Errorf("Expected name %q, got %q", tt.expectedName, prog.Name())
			}
		})
	}
}
