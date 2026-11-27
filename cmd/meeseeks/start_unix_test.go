//go:build !windows

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

func TestStartCommand_Foreground(t *testing.T) {
	configDir := setMeeseeksConfigDirForTest(t)

	configFile := filepath.Join(configDir, "config.yaml")
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
	cmd.Args = append(cmd.Args, []string{"start"}...)
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
