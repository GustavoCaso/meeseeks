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

	"github.com/GustavoCaso/meeseeks/internal/config"
	"github.com/GustavoCaso/meeseeks/internal/logger"
)

// runCLICommand runs CLI commands as subprocess.
func runCLICommand(
	args []string,
	stdoutBuf io.Writer,
	stderrBuf io.Writer,
	timeout time.Duration,
) int {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

// newTestServer creates and starts a server with the given config content.
func newTestServer(t *testing.T, configContent string) {
	t.Helper()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		t.Fatalf("failed to load test config: %v", err)
	}

	// Make sure running tests while having a production
	// instance of meeseeks running do not cause problems
	customDir := "/tmp/meeseeks"
	t.Setenv("MEESEEKS_CONFIG_DIR", customDir)

	server, err := startServer(t.Context(), cfg, logger.New(), getSocketPath())
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	// Set up cleanup
	t.Cleanup(func() {
		os.RemoveAll(customDir)
		server.Stop()
	})
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
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdoutBuf, stderrBuf bytes.Buffer
			exitCode := runCLICommand(tt.args, &stdoutBuf, &stderrBuf, 5*time.Second)
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
	t.Helper()

	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runCLICommand([]string{command, "-h"}, &stdoutBuf, &stderrBuf, 5*time.Second)
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
