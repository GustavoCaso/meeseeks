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

func TestStartCommand_ConfigValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		args         []string
		expectedExit int
		errorMessage string
	}{
		{
			name:         "nonexistent config file",
			args:         []string{"start", "-config", "/nonexistent/file.yaml"},
			expectedExit: 1,
			errorMessage: "failed to load config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdoutBuf, stderrBuf bytes.Buffer
			exitCode := runCLICommand(tt.args, &stdoutBuf, &stderrBuf, 5*time.Second)
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
		"-config",
		"-d",
	})
}

func TestStartCommand_Foreground(t *testing.T) {
	setMeeseeksConfigDirForTest(t)

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

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Args = append(cmd.Args, []string{"start", "-config", configFile}...)
	cmd.Env = os.Environ()

	// Set process group to ensure we can kill child processes
	// Running test creates a chain of processes:
	// 1. go run . (parent)
	// 2. The compiled meeseeks binary (child)
	// 3. Potentially other child processes started by meeseeks
	// By using syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL), we kill the entire process group
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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
		t.Fatalf("error starting in foreground: %s", err.Error())
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

	// Send SIGTERM after allowing time for startup message
	terminateProcess := func() {
		time.Sleep(1 * time.Second) // Give more time for startup
		if cmd.Process != nil {
			// First try SIGTERM to the process group
			syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

			// Give it time to exit gracefully, then force kill the process group
			time.Sleep(300 * time.Millisecond)
			if cmd.Process != nil {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}
	}
	go terminateProcess()

	err = cmd.Wait()

	// Wait for all goroutines to finish reading before accessing buffers
	// This prevents the data race between concurrent writes (ReadFrom) and reads (String)
	wg.Wait()

	if err != nil && !strings.Contains(err.Error(), "signal") &&
		!strings.Contains(err.Error(), "interrupt") &&
		!strings.Contains(err.Error(), "context deadline") {
		t.Fatalf("Unexpected error (ignoring interrupt/timeout/killed): %v", err)
	}

	output := stdout.String() + stderr.String()
	expected := "\"Started meeseeks\""
	if !strings.Contains(output, expected) {
		t.Fatalf("Expected output to contain %q, got %q", expected, output)
	}
}

func TestStartCommand_Detached(t *testing.T) {
	setMeeseeksConfigDirForTest(t)

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
		[]string{"start", "-d", "-config", configFile},
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
