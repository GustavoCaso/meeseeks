package main

import (
	"os"
	"path/filepath"
	"testing"
)

//nolint:tparallel // the test uses Setenv
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
		t.Parallel()
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
