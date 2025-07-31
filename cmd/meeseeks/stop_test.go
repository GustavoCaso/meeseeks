package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

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
			errorMessage: "meeseeks server not running",
		},
		{
			name:         "stop specific program with no daemon",
			args:         []string{"stop", "test-program"},
			expectedExit: 1,
			errorMessage: "meeseeks server not running",
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