package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/config"
)

// Helper function to run CLI commands as subprocess.
func runCLI(t *testing.T, args []string, timeout time.Duration) (string, string, int) {
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "main.go")
	cmd.Args = append(cmd.Args, args...)
	cmd.Dir = "/Users/gustavocaso/src/github.com/GustavoCaso/meeseeks/cmd/meeseeks"

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

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

	return stdoutBuf.String(), stderrBuf.String(), exitCode
}

func TestMain_VersionCommand(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedOutput string
		expectedExit   int
	}{
		{
			name:           "version command",
			args:           []string{"version"},
			expectedOutput: "meeseeks version 1.0.0",
			expectedExit:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runCLI(t, tt.args, 5*time.Second)
			output := stdout + stderr

			if exitCode != tt.expectedExit {
				t.Errorf("Expected exit code %d, got %d", tt.expectedExit, exitCode)
			}

			if !strings.Contains(output, tt.expectedOutput) {
				t.Errorf("Expected output to contain %q, got %q", tt.expectedOutput, output)
			}
		})
	}
}

func TestMain_NoCommand(t *testing.T) {
	stdout, stderr, exitCode := runCLI(t, []string{}, 5*time.Second)
	output := stdout + stderr

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
}

func TestMain_UnknownCommand(t *testing.T) {
	stdout, stderr, exitCode := runCLI(t, []string{"unknown"}, 5*time.Second)
	output := stdout + stderr

	if exitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(output, "Unknown command: unknown") {
		t.Errorf("Expected unknown command error, got %q", output)
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
			stdout, stderr, exitCode := runCLI(t, tt.args, 5*time.Second)
			output := stdout + stderr

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

func TestRunCommand_DetachedMode(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-detached-config.yaml")

	configContent := `programs:
  - name: "test-echo-detached"
    command: "echo"
    args: ["detached", "test"]
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
	}{
		{
			name: "detached run",
			args: []string{"run", "-d", "-config", configFile},
			expectedOutput: []string{
				"Starting meeseeks daemon",
			},
			timeout: 3 * time.Second,
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

			// Send interrupt after a short delay to stop the daemon
			go func() {
				time.Sleep(500 * time.Millisecond)
				if cmd.Process != nil {
					cmd.Process.Signal(os.Interrupt)
				}
			}()

			err := cmd.Run()

			output := stdout.String() + stderr.String()

			for _, expected := range tt.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, got %q", expected, output)
				}
			}

			// Ignore interrupt signals and timeouts
			if err != nil && !strings.Contains(err.Error(), "signal") &&
				!strings.Contains(err.Error(), "interrupt") &&
				!strings.Contains(err.Error(), "context deadline") {
				t.Errorf("Unexpected error (ignoring interrupt/timeout): %v", err)
			}
		})
	}
}

func TestRunCommand_Help(t *testing.T) {
	stdout, stderr, exitCode := runCLI(t, []string{"run", "-h"}, 5*time.Second)
	output := stdout + stderr

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
			errorMessage: "failed to connect to daemon",
		},
		{
			name:         "status specific program with no daemon",
			args:         []string{"status", "test-program"},
			expectedExit: 1,
			errorMessage: "failed to connect to daemon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runCLI(t, tt.args, 5*time.Second)
			output := stdout + stderr

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
	stdout, stderr, exitCode := runCLI(t, []string{"status", "-h"}, 5*time.Second)
	output := stdout + stderr

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
			errorMessage: "failed to connect to daemon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runCLI(t, tt.args, 5*time.Second)
			output := stdout + stderr

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
	stdout, stderr, exitCode := runCLI(t, []string{"logs", "-h"}, 5*time.Second)
	output := stdout + stderr

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
			errorMessage: "failed to connect to daemon",
		},
		{
			name:         "stop specific program with no daemon",
			args:         []string{"stop", "test-program"},
			expectedExit: 1,
			errorMessage: "failed to connect to daemon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runCLI(t, tt.args, 5*time.Second)
			output := stdout + stderr

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
	stdout, stderr, exitCode := runCLI(t, []string{"stop", "-h"}, 5*time.Second)
	output := stdout + stderr

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
