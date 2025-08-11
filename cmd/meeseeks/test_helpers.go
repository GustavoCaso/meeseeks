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
)

// runCLICommand runs CLI commands as subprocess.
func runCLICommand(
	t *testing.T,
	args []string,
	stdoutBuf io.Writer,
	stderrBuf io.Writer,
	timeout time.Duration,
) int {
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Args = append(cmd.Args, args...)

	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = 1
		}
	}

	return exitCode
}

// testDetachedDaemon manages a detached daemon for testing.
type testDetachedDaemon struct {
	t          *testing.T
	configFile string
	started    bool
}

// newTestDetachedDaemon creates and starts a detached daemon with the given config content.
func newTestDetachedDaemon(t *testing.T, configContent string) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	daemon := &testDetachedDaemon{
		t:          t,
		configFile: configFile,
	}

	daemon.start()
}

// start starts the detached daemon.
func (d *testDetachedDaemon) start() {
	if d.started {
		return
	}

	expectedPidFile := getPidFile()
	expectedSocketPath := getSocketPath()

	os.Remove(expectedPidFile)
	os.Remove(expectedSocketPath)

	var stdout, stderr bytes.Buffer
	exitCode := runCLICommand(
		d.t,
		[]string{"run", "-d", "-config", d.configFile},
		&stdout,
		&stderr,
		15*time.Second,
	)

	if exitCode != 0 {
		d.t.Fatalf(
			"Failed to start daemon: exit code %d\nStdout: %s\nStderr: %s\nConfig: %s\nPID file: %s\nSocket: %s",
			exitCode,
			stdout.String(),
			stderr.String(),
			d.configFile,
			getPidFile(),
			getSocketPath(),
		)
	}

	d.started = true

	// Set up cleanup - use defer instead of Cleanup to ensure immediate cleanup
	// after test function completes, not after all subtests
	d.t.Cleanup(func() {
		d.stop()
	})

	// Give daemon time to fully start
	time.Sleep(500 * time.Millisecond)
}

// stop stops the detached daemon.
func (d *testDetachedDaemon) stop() {
	if !d.started {
		return
	}

	// Try to exit gracefully first
	exitCode := runCLICommand(d.t, []string{"exit"}, nil, nil, 5*time.Second)
	if exitCode != 0 && exitCode != 1 { // exit might return 1 if already stopped
		d.t.Logf("Unexpected exit code during daemon cleanup: %d", exitCode)
	}

	// Ensure cleanup by removing PID and socket files
	expectedPidFile := getPidFile()
	expectedSocketPath := getSocketPath()
	os.Remove(expectedPidFile)
	os.Remove(expectedSocketPath)

	d.started = false
}

// ensureNoDaemonRunning ensures no daemon is running - useful for validation tests.
func ensureNoDaemonRunning(t *testing.T) {
	expectedPidFile := getPidFile()
	expectedSocketPath := getSocketPath()

	if _, err := os.Stat(expectedPidFile); !os.IsNotExist(err) {
		t.Fatal("PID file still exists. Stoping meeseeks should remove the PID file")
	}

	if _, err := os.Stat(expectedSocketPath); !os.IsNotExist(err) {
		t.Fatal("Socket file still exists. Stoping meeseeks should remove the Socket file")
	}
}

// commandTestCase represents a test case for command testing.
type commandTestCase struct {
	name          string
	args          []string
	expectedExit  int
	shouldContain string
}

// runCommandTests runs a set of command test cases.
func runCommandTests(t *testing.T, tests []commandTestCase) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdoutBuf, stderrBuf bytes.Buffer
			exitCode := runCLICommand(t, tt.args, &stdoutBuf, &stderrBuf, 5*time.Second)
			output := stdoutBuf.String() + stderrBuf.String()

			if exitCode != tt.expectedExit {
				t.Fatalf("Expected exit code %d, got %d", tt.expectedExit, exitCode)
			}

			if !strings.Contains(output, tt.shouldContain) {
				t.Fatalf("Expected output to contain %q, got %q", tt.shouldContain, output)
			}
		})
	}
}

// testCommandHelp tests the help functionality for a command.
func testCommandHelp(t *testing.T, command string, expectedMessages []string) {
	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runCLICommand(t, []string{command, "-h"}, &stdoutBuf, &stderrBuf, 5*time.Second)
	output := stdoutBuf.String() + stderrBuf.String()

	if exitCode != 0 {
		t.Fatalf("Expected exit code 0 for help, got %d", exitCode)
	}

	for _, msg := range expectedMessages {
		if !strings.Contains(output, msg) {
			t.Fatalf("Expected help output to contain %q, got %q", msg, output)
		}
	}
}
