package main

import (
	"strings"
	"testing"

	"github.com/GustavoCaso/meeseeks/internal/config"
)

func TestCreateProgramFromConfig(t *testing.T) {
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
				Name:     "test-invalid",
				Command:  "echo",
				Interval: "invalid-duration",
			},
			expectError:  true,
			errorMessage: "invalid interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := createProgramFromConfig(tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errorMessage) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMessage, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if prog.Name() != tt.expectedName {
				t.Errorf("Expected name %q, got %q", tt.expectedName, prog.Name())
			}
		})
	}
}
