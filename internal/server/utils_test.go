package server

import (
	"testing"

	"github.com/GustavoCaso/meeseeks/internal/config"
	"github.com/GustavoCaso/meeseeks/internal/logger"
)

func TestCreateProgramFromConfig(t *testing.T) {
	t.Parallel()
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
				Name:     "test-interval",
				Command:  "echo",
				Interval: "100ks",
			},
			expectError:  true,
			errorMessage: "time: unknown unit \"ks\" in duration \"100ks\"",
		},
		{
			name: "program with initial delay",
			config: config.ProgramConfig{
				Name:         "test-initial-delay",
				Command:      "echo",
				InitialDelay: "30s",
			},
			expectedName: "test-initial-delay",
		},
		{
			name: "program with invalid initial delay",
			config: config.ProgramConfig{
				Name:         "test-initial-delay",
				Command:      "echo",
				InitialDelay: "100ks",
			},
			expectError:  true,
			errorMessage: "time: unknown unit \"ks\" in duration \"100ks\"",
		},
		{
			name: "program with stdout file",
			config: config.ProgramConfig{
				Name:    "test-stdout-file",
				Command: "echo",
				Args:    []string{"hello"},
				Stdout:  "/tmp/test_stdout.log",
			},
			expectedName: "test-stdout-file",
		},
		{
			name: "program with stderr file",
			config: config.ProgramConfig{
				Name:    "test-stderr-file",
				Command: "bash",
				Args:    []string{"-c", "echo 'error' >&2"},
				Stderr:  "/tmp/test_stderr.log",
			},
			expectedName: "test-stderr-file",
		},
		{
			name: "program with both stdout and stderr files",
			config: config.ProgramConfig{
				Name:    "test-dual-files",
				Command: "bash",
				Args:    []string{"-c", "echo 'output'; echo 'error' >&2"},
				Stdout:  "/tmp/test_dual_stdout.log",
				Stderr:  "/tmp/test_dual_stderr.log",
			},
			expectedName: "test-dual-files",
		},
		{
			name: "program with all options",
			config: config.ProgramConfig{
				Name:          "test-all-options",
				Command:       "bash",
				Args:          []string{"-c", "echo $TEST_VAR; echo 'error' >&2"},
				Env:           []string{"TEST_VAR=full_test"},
				KeepStdinOpen: true,
				Stdout:        "/tmp/test_all_stdout.log",
				Stderr:        "/tmp/test_all_stderr.log",
				Interval:      "60s",
				InitialDelay:  "10s",
			},
			expectedName: "test-all-options",
		},
	}

	logger := logger.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prog, err := createProgramFromConfig(tt.config, logger)

			if tt.expectError {
				if err == nil {
					t.Fatalf("Expected error but got none")
				}

				if prog != nil {
					t.Fatalf("Expected nil program but got %+v", prog)
				}

				if err.Error() != tt.errorMessage {
					t.Fatalf("Expected error message %q, got %q", tt.errorMessage, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if prog.Name() != tt.expectedName {
				t.Fatalf("Expected name %q, got %q", tt.expectedName, prog.Name())
			}
		})
	}
}
