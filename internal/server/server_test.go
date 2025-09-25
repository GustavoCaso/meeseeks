package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

func TestServer_HTTPHandlers(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "meeseeks.sock")
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `programs:
  - name: "test-program1"
    command: "echo"
    args: ["hello"]
  - name: "test-program2"
    command: "echo"
    args: ["world"]
`

	if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	s, err := New(sockPath, configFile, logger.New(), time.Millisecond)
	if err != nil {
		t.Fatalf("creating server failed: %s", err.Error())
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

	err = s.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	t.Cleanup(func() {
		s.Stop()
		cancel()
	})

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	client := NewClient(ctx, sockPath)

	tests := []struct {
		name   string
		testFn func(t *testing.T, client *Client)
	}{
		{
			name: "statistics all programs",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Statistics("")
				if err != nil {
					t.Fatalf("Statistics() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Fatalf("Expected success=true, got %v", resp.Success)
				}
				if resp.Data == nil {
					t.Fatalf("Expected data to be non-nil")
				}
			},
		},
		{
			name: "statistics specific program",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Statistics("test-program1")
				if err != nil {
					t.Fatalf("Statistics() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Fatalf("Expected success=true, got %v", resp.Success)
				}
				if resp.Data == nil {
					t.Fatalf("Expected data to be non-nil")
				}
			},
		},
		{
			name: "statistics nonexistent program",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Statistics("nonexistent")
				if err != nil {
					t.Fatalf("Statistics() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Fatalf("Expected success=false, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Fatalf("Expected error message to be non-empty")
				}
			},
		},
		{
			name: "logs with program name",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Logs("test-program1")
				if err != nil {
					t.Fatalf("Logs() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Fatalf("Expected success=true, got %v", resp.Success)
				}
				if resp.Data == nil {
					t.Fatalf("Expected data to be non-nil")
				}
			},
		},
		{
			name: "logs nonexistent program",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Logs("nonexistent")
				if err != nil {
					t.Fatalf("Logs() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Fatalf("Expected success=false, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Fatalf("Expected error message to be non-empty")
				}
			},
		},
		{
			name: "logs no program",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Logs("")
				if err != nil {
					t.Fatalf("Logs() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Fatalf("Expected success=false, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Fatalf("Expected error message to be non-empty")
				}
			},
		},
		{
			name: "stop command without program name",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Stop("", "5s")
				if err != nil {
					t.Fatalf("Stop() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Fatalf("Expected success=false for empty program name, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Fatalf("Expected error message for empty program name")
				}
			},
		},
		{
			name: "stop command invalid timeout",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Stop("test-program2", "hello")
				if err != nil {
					t.Fatalf("Stop() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Fatalf("Expected success=false for invalid timeout, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Fatalf("Expected error message for empty program name")
				}
			},
		},
		{
			name: "stop request with program name",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Stop("test-program2", "5s")
				if err != nil {
					t.Fatalf("Stop() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Fatalf("Stop() expected success=true, got %v", resp.Success)
				}
				if resp.Error != "" {
					t.Fatalf("Expected error message to be non-empty")
				}
			},
		},
		{
			name: "run with empty program name",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.RunProgram("")
				if err != nil {
					t.Fatalf("RunProgram() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Fatalf("RunProgram() expected success=false, got %v", resp.Success)
				}
				if !strings.Contains(resp.Error, "program name required") {
					t.Fatalf("Expected error message %q, got %q", "program name required", resp.Error)
				}
			},
		},
		{
			name: "run with non existing program name",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.RunProgram("nonexistent-program")
				if err != nil {
					t.Fatalf("RunProgram() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Fatalf("RunProgram() expected success=false, got %v", resp.Success)
				}
				if !strings.Contains(resp.Error, "program nonexistent-program not present") {
					t.Fatalf("Expected error message %q, got %q", "program nonexistent-program not present", resp.Error)
				}
			},
		},
		{
			name: "run program successfully",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.RunProgram("test-program1")
				if err != nil {
					t.Fatalf("RunProgram() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Fatalf("RunProgram() expected success=true, got %v", resp.Success)
				}
				if resp.Error != "" {
					t.Fatalf("Expected error message to be non-empty")
				}
			},
		},
		{
			name: "reload config. invalid timeout",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Reload("hello")
				if err != nil {
					t.Fatalf("RunProgram() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Fatalf("Reload() expected success=false, got %v", resp.Success)
				}
				if !strings.Contains(resp.Error, "error parsing timeout") {
					t.Fatalf("Expected error message %q, got %q", "error parsing timeout", resp.Error)
				}
			},
		},
		{
			name: "reload config",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Reload("1s")
				if err != nil {
					t.Fatalf("RunProgram() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Fatalf("Reload() expected success=true, got %v", resp.Success)
				}
				if resp.Error != "" {
					t.Fatalf("Expected error message to be non-empty")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFn(t, client)
		})
	}
}

func TestServer_FailLoadConfigInvalidInterval(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "meeseeks.sock")
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `programs:
  - name: "test-program1"
    command: "echo"
    args: ["hello"]
    interval: "invalid"
`

	if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	_, err := New(sockPath, configFile, logger.New(), time.Millisecond)
	if err == nil {
		t.Fatal("Expected error when parsing configuration. got nil")
	}

	if !strings.Contains(err.Error(), "invalid interval for program") {
		t.Fatalf("Expected error message to include 'invalid interval for program', got: %s", err.Error())
	}
}

func TestServer_FailLoadConfigNotPresent(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "meeseeks.sock")
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	_, err := New(sockPath, configFile, logger.New(), time.Millisecond)
	if err == nil {
		t.Fatal("Expected error when parsing configuration. got nil")
	}

	if !strings.Contains(err.Error(), "failed to load config") {
		t.Fatalf("Expected error message to include 'failed to load config', got: %s", err.Error())
	}
}

func TestClient_ConnectToNonExistentDaemon(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewClient(ctx, "/nonexistent/path.sock")

	_, err := client.Statistics("")
	if err == nil {
		t.Fatalf("Expected error when connecting to non-existent daemon")
	}

	expectedMsg := "meeseeks server not running (socket not found)"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Fatalf("Error should contain %q, got %q", expectedMsg, err.Error())
	}
}

func TestServer_ConcurrentConnections(t *testing.T) {
	t.Parallel()
	tmpDir := filepath.Join("/tmp/", t.Name())
	err := os.MkdirAll(tmpDir, 0750)
	if err != nil {
		t.Fatalf("error creating temp folder %s", err.Error())
	}
	sockPath := filepath.Join(tmpDir, "meeseeks.sock")

	configFile := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `programs:
  - name: "concurrent-test"
    command: "echo"
    args: ["concurrent"]
`

	if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	s, err := New(sockPath, configFile, logger.New(), time.Millisecond)
	if err != nil {
		t.Fatalf("creating server failed: %s", err.Error())
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	err = s.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}

	t.Cleanup(func() {
		s.Stop()
		os.RemoveAll(tmpDir)
	})

	// Create multiple clients concurrently
	numClients := 5
	results := make(chan error, numClients)

	for i := range numClients {
		go func(_ int) {
			client := NewClient(ctx, sockPath)
			resp, err := client.Statistics("")
			if err != nil {
				results <- err
				return
			}
			if !resp.Success {
				results <- err
				return
			}
			results <- nil
		}(i)
	}

	// Wait for all clients to complete
	for i := range numClients {
		err := <-results
		if err != nil {
			t.Fatalf("Client %d failed: %v", i, err)
		}
	}
}

// Benchmark server request handling.
func BenchmarkServer_HandleRequest(b *testing.B) {
	tmpDir := filepath.Join("/tmp/", b.Name())
	err := os.MkdirAll(tmpDir, 0750)
	if err != nil {
		b.Fatalf("error creating temp folder %s", err.Error())
	}
	sockPath := filepath.Join(tmpDir, "meeseeks.sock")

	configFile := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `programs:
  - name: "bench-program"
    command: "echo"
    args: ["benchmark"]
`

	if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
		b.Fatalf("Failed to create test config file: %v", err)
	}

	s, err := New(sockPath, configFile, logger.New(), time.Millisecond)
	if err != nil {
		b.Fatalf("creating server failed: %s", err.Error())
	}

	ctx, cancel := context.WithTimeout(b.Context(), 30*time.Second)
	defer cancel()

	err = s.Start(ctx)
	if err != nil {
		b.Fatalf("Failed to start server: %v", err)
	}

	b.Cleanup(func() {
		s.Stop()
		os.RemoveAll(tmpDir)
	})

	time.Sleep(100 * time.Millisecond) // Give server time to start

	client := NewClient(ctx, sockPath)

	b.ResetTimer()
	for range b.N {
		_, _ = client.Statistics("")
	}
}
