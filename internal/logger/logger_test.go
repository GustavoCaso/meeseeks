package logger

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   slog.Level
	}{
		{
			name: "debug level",
			config: Config{
				Level:  LevelDebug,
				Format: FormatText,
				Output: "stdout",
			},
			want: slog.LevelDebug,
		},
		{
			name: "info level",
			config: Config{
				Level:  LevelInfo,
				Format: FormatText,
				Output: "stdout",
			},
			want: slog.LevelInfo,
		},
		{
			name: "warn level",
			config: Config{
				Level:  LevelWarn,
				Format: FormatText,
				Output: "stdout",
			},
			want: slog.LevelWarn,
		},
		{
			name: "error level",
			config: Config{
				Level:  LevelError,
				Format: FormatText,
				Output: "stdout",
			},
			want: slog.LevelError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New(tt.config)
			if logger.Logger == nil {
				t.Error("Expected logger to be created")
			}
		})
	}
}

func TestJSONFormat(t *testing.T) {
	// Create a logger that writes to our buffer
	config := Config{
		Level:  LevelInfo,
		Format: FormatJSON,
		Output: "stdout",
	}

	// Temporarily replace stdout to capture output
	original := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	logger := New(config)
	logger.Info("test message", "key", "value")

	// Restore stdout and read from pipe
	w.Close()
	os.Stdout = original

	output, _ := io.ReadAll(r)

	// Verify it's valid JSON
	var logEntry map[string]interface{}
	if err := json.Unmarshal(output, &logEntry); err != nil {
		t.Errorf("Expected valid JSON output, got error: %v", err)
	}

	// Check for expected fields
	if logEntry["msg"] != "test message" {
		t.Errorf("Expected msg to be 'test message', got %v", logEntry["msg"])
	}

	if logEntry["key"] != "value" {
		t.Errorf("Expected key to be 'value', got %v", logEntry["key"])
	}
}

func TestTextFormat(t *testing.T) {
	// Temporarily replace stdout to capture output
	original := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	config := Config{
		Level:  LevelInfo,
		Format: FormatText,
		Output: "stdout",
	}

	logger := New(config)
	logger.Info("test message", "key", "value")

	// Restore stdout and read from pipe
	w.Close()
	os.Stdout = original

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	// Check for expected content in text format
	if !strings.Contains(outputStr, "test message") {
		t.Errorf("Expected output to contain 'test message', got %s", outputStr)
	}

	if !strings.Contains(outputStr, "key=value") {
		t.Errorf("Expected output to contain 'key=value', got %s", outputStr)
	}
}
