package main

import (
	"os"
	"path/filepath"
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
					t.Fatalf("Expected error but got none")
				}
				if !strings.Contains(err.Error(), tt.errorMessage) {
					t.Fatalf("Expected error containing %q, got %q", tt.errorMessage, err.Error())
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

func TestGetMeeseeksDir(t *testing.T) {
	t.Run("uses environment variable when set", func(t *testing.T) {
		customDir := "/custom/meeseeks"
		t.Setenv("MEESEEKS_CONFIG_DIR", customDir)

		result := getMeeseeksDir()
		if result != customDir {
			t.Fatalf("Expected %q, got %q", customDir, result)
		}
	})

	t.Run("uses default when environment variable not set", func(t *testing.T) {
		result := getMeeseeksDir()
		homeDir, _ := os.UserHomeDir()
		expected := filepath.Join(homeDir, ".meeseeks")

		if result != expected {
			t.Fatalf("Expected %q, got %q", expected, result)
		}
	})
}

func TestPathFunctions(t *testing.T) {
	t.Run("all paths use custom MEESEEKS_CONFIG_DIR when set", func(t *testing.T) {
		customDir := "/custom/meeseeks"
		t.Setenv("MEESEEKS_CONFIG_DIR", customDir)

		tests := []struct {
			name     string
			function func() string
			expected string
		}{
			{"socket path", getSocketPath, filepath.Join(customDir, "meeseeks.sock")},
			{"pid file", getPidFile, filepath.Join(customDir, "meeseeks.pid")},
			{"config path", getDefaultConfigPath, filepath.Join(customDir, "config.yaml")},
			{"log file", getLogFilePath, filepath.Join(customDir, "meeseeks.log")},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := tt.function()
				if result != tt.expected {
					t.Fatalf("Expected %q, got %q", tt.expected, result)
				}
			})
		}
	})

	t.Run("all paths use default directory when environment variable not set", func(t *testing.T) {
		homeDir, _ := os.UserHomeDir()
		defaultDir := filepath.Join(homeDir, ".meeseeks")

		tests := []struct {
			name     string
			function func() string
			expected string
		}{
			{"socket path", getSocketPath, filepath.Join(defaultDir, "meeseeks.sock")},
			{"pid file", getPidFile, filepath.Join(defaultDir, "meeseeks.pid")},
			{"config path", getDefaultConfigPath, filepath.Join(defaultDir, "config.yaml")},
			{"log file", getLogFilePath, filepath.Join(defaultDir, "meeseeks.log")},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := tt.function()
				if result != tt.expected {
					t.Fatalf("Expected %q, got %q", tt.expected, result)
				}
			})
		}
	})
}
