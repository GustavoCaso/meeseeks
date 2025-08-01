package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

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

func TestRunCommand_Foreground(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Args = append(cmd.Args, []string{"run", "-config", configFile}...)
	cmd.Dir = "/Users/gustavocaso/src/github.com/GustavoCaso/meeseeks/cmd/meeseeks"

	// Use pipes instead of bytes.Buffer to avoid deadlock.
	// When using bytes.Buffer directly with cmd.Stdout/Stderr, the process can block
	// writing to the buffer if nothing is reading from it. This causes the test to hang
	// when the daemon process tries to output messages but the test isn't actively
	// reading from the buffers before calling cmd.Wait().
	//
	// Pipes create separate file descriptors that are read asynchronously in goroutines,
	// preventing the write operations from blocking and avoiding the deadlock.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to create stderr pipe: %v", err)
	}

	var stdout, stderr bytes.Buffer
	var wg sync.WaitGroup

	err = cmd.Start()
	if err != nil {
		t.Errorf("error starting in foreground: %s", err.Error())
	}

	// Start goroutines to read from pipes and populate buffers
	// Use WaitGroup to ensure all reading is complete before accessing buffers
	wg.Add(2)
	go func() {
		defer wg.Done()
		stdout.ReadFrom(stdoutPipe)
	}()
	go func() {
		defer wg.Done()
		stderr.ReadFrom(stderrPipe)
	}()

	// Send SIGTERM after a short delay to gracefully stop the daemon
	go func() {
		time.Sleep(500 * time.Millisecond)
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
		}
	}()

	err = cmd.Wait()

	// Wait for all goroutines to finish reading before accessing buffers
	// This prevents the data race between concurrent writes (ReadFrom) and reads (String)
	wg.Wait()

	if err != nil && !strings.Contains(err.Error(), "signal") &&
		!strings.Contains(err.Error(), "interrupt") &&
		!strings.Contains(err.Error(), "context deadline") {
		t.Errorf("Unexpected error (ignoring interrupt/timeout): %v", err)
	}

	output := stdout.String() + stderr.String()
	expected := "Started meeseeks program_count=1"
	if !strings.Contains(output, expected) {
		t.Errorf("Expected output to contain %q, got %q", expected, output)
	}
}

func TestRunCommand_Detached(t *testing.T) {
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

	expectedPidFile := getPidFile()
	expectedSocketPath := getSocketPath()

	os.Remove(expectedPidFile)
	os.Remove(expectedSocketPath)

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
	if !strings.Contains(output, "Started meeseeks (detached)") {
		t.Errorf("Expected `Started meeseeks (detached)`, got: %q", output)
	}

	if _, err := os.Stat(expectedPidFile); os.IsNotExist(err) {
		t.Errorf("PID file was not created at %s", expectedPidFile)
	}

	time.Sleep(500 * time.Millisecond)

	if _, err := os.Stat(expectedSocketPath); os.IsNotExist(err) {
		t.Errorf("Socket file was not created at %s", expectedSocketPath)
	}

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

	if strings.Contains(statusOutput, "meeseeks server not running") {
		t.Errorf("Status command could not connect to daemon: %q", statusOutput)
	}

	exitCode = runCLICommand(t, []string{"exit"}, nil, nil, 5*time.Second)
	if exitCode != 0 {
		t.Errorf("Expected exit code %d, got %d", 0, exitCode)
	}

	if _, err := os.Stat(expectedPidFile); !os.IsNotExist(err) {
		t.Error("PID file still exists. Stoping meeseeks should remove the PID file")
	}

	if _, err := os.Stat(expectedSocketPath); !os.IsNotExist(err) {
		t.Error("Socket file still exists. Stoping meeseeks should remove the Socket file")
	}

	var stdoutBuf_2, stderrBuf_2 bytes.Buffer
	exitCode = runCLICommand(
		t,
		[]string{"status"},
		&stdoutBuf_2,
		&stderrBuf_2,
		5*time.Second,
	)
	if exitCode != 1 {
		t.Errorf("Expected exit code %d, got %d", 1, exitCode)
	}
	statusOutput = stdoutBuf_2.String() + stderrBuf_2.String()

	if !strings.Contains(statusOutput, "meeseeks server not running") {
		t.Error("Status command should not work after exiting meeseeks")
	}
}
