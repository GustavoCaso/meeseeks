package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestMainCommands(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.test(t)
		})
	}
}
