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

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

func runCLICommand(
	args []string,
	stdoutBuf io.Writer,
	stderrBuf io.Writer,
	timeout time.Duration,
	extraEnv ...string,
) int {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Args = append(cmd.Args, args...)
	env := os.Environ()
	env = append(env, extraEnv...)
	cmd.Env = env

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

// waitFor polls cond every 10ms until it returns true or timeout expires,
// failing the test with msg on timeout.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

func setMeeseeksConfigDirForTest(t *testing.T) {
	t.Helper()

	// Make sure running tests while having a production
	// instance of meeseeks running do not cause problems
	customDir := filepath.Join("/tmp/", t.Name())

	err := os.MkdirAll(customDir, 0750)
	if err != nil {
		t.Fatalf("error creating temp folder for tests %s", err.Error())
	}

	t.Setenv("MEESEEKS_CONFIG_DIR", customDir)

	t.Cleanup(func() {
		os.RemoveAll(customDir)
	})
}

// newTestServer creates and starts a server with the given config content.
func newTestServer(t *testing.T, configPath, configContent string) {
	t.Helper()

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	tempFolder := filepath.Join("/tmp/", t.Name())
	err := os.MkdirAll(tempFolder, 0750)
	if err != nil {
		t.Fatalf("failed to create temp folder for socket: %s", err.Error())
	}

	socketPath := filepath.Join(tempFolder, "meeseeks.sock")

	t.Setenv("MEESEEKS_CONFIG_DIR", tempFolder)

	server, err := startServer(
		t.Context(),
		configPath,
		logger.New(),
		socketPath,
		30*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	// Set up cleanup
	t.Cleanup(func() {
		server.Stop()
		os.RemoveAll(tempFolder)
	})
}

// commandTestCase represents a test case for command testing.
type commandTestCase struct {
	name             string
	args             []string
	expectedExit     int
	shouldContain    string
	shouldNotContain string
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
				t.Fatalf(
					"Expected exit code %d, got %d. Output: %s",
					tt.expectedExit,
					exitCode,
					output,
				)
			}

			if tt.shouldContain != "" {
				if !strings.Contains(output, tt.shouldContain) {
					t.Fatalf("Expected output to contain %q, got %q", tt.shouldContain, output)
				}
			}

			if tt.shouldNotContain != "" {
				if strings.Contains(output, tt.shouldNotContain) {
					t.Fatalf(
						"Expected output to not contain %q, got %q",
						tt.shouldNotContain,
						output,
					)
				}
			}
		})
	}
}

// testCommandHelp tests the help functionality for a command.
func testCommandHelp(t *testing.T, command []string, expectedMessages []string) {
	t.Helper()

	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runCLICommand(append(command, "-h"), &stdoutBuf, &stderrBuf, 5*time.Second)
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
