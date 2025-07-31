package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

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
			errorMessage: "meeseeks server not running",
		},
		{
			name:         "status specific program with no daemon",
			args:         []string{"status", "test-program"},
			expectedExit: 1,
			errorMessage: "meeseeks server not running",
		},
		{
			name:         "status with invalid format",
			args:         []string{"status", "-format", "invalid"},
			expectedExit: 1,
			errorMessage: "invalid format: invalid. Valid formats: table, json",
		},
		{
			name:         "status with invalid format shorthand",
			args:         []string{"status", "-f", "xml"},
			expectedExit: 1,
			errorMessage: "invalid format: xml. Valid formats: table, json",
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
		"Usage: meeseeks status [options] [program_name]",
		"Show status of running programs",
		"-format",
		"-f",
	}

	for _, msg := range expectedMessages {
		if !strings.Contains(output, msg) {
			t.Errorf("Expected help output to contain %q, got %q", msg, output)
		}
	}
}