package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

func TestServer_StartStop(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func() string
		wantErr bool
	}{
		{
			name: "successful start and stop",
			setup: func() string {
				tmpDir := t.TempDir()
				return filepath.Join(tmpDir, "test.sock")
			},
		},
		{
			name: "start with existing socket file",
			setup: func() string {
				tmpDir := t.TempDir()
				sockPath := filepath.Join(tmpDir, "test.sock")
				// Create existing socket file
				_, err := os.Create(sockPath)
				if err != nil {
					t.Fatalf("Failed to create existing socket file: %v", err)
				}
				return sockPath
			},
		},
		{
			name: "invalid socket directory",
			setup: func() string {
				return "/nonexistent/directory/test.sock"
			},
			wantErr: true,
		},
	}

	logger := logger.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sockPath := tt.setup()
			configFile := filepath.Join(t.TempDir(), "test-config.yaml")

			configContent := `programs:
  - name: "test-program1"
    command: "echo"
    args: ["hello"]
`

			if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
				t.Fatalf("Failed to create test config file: %v", err)
			}

			s, err := New(sockPath, configFile, logger, time.Millisecond)
			if err != nil {
				t.Fatalf("creating server failed: %s", err.Error())
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			err = s.Start(ctx)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Start() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Start() unexpected error = %v", err)
				return
			}

			// Verify socket file exists
			if _, err := os.Stat(sockPath); os.IsNotExist(err) {
				t.Fatalf("Socket file was not created at %s", sockPath)
			}

			// Test double start (should return error)
			err = s.Start(ctx)
			if err == nil {
				t.Fatalf("Second Start() should return error but got none")
			}

			// Stop server
			err = s.Stop()
			if err != nil {
				t.Fatalf("Stop() unexpected error = %v", err)
			}

			// Verify socket file is removed
			if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
				t.Fatalf("Socket file should be removed after Stop()")
			}

			// Test double stop (should not error)
			err = s.Stop()
			if err != nil {
				t.Fatalf("Second Stop() unexpected error = %v", err)
			}
		})
	}
}

func TestServer_AddProgramAndStart(t *testing.T) {
	t.Parallel()
	tmpDir := filepath.Join("/tmp/", t.Name())
	err := os.MkdirAll(tmpDir, 0750)
	if err != nil {
		t.Fatalf("error creating temp folder %s", err.Error())
	}
	sockPath := filepath.Join(tmpDir, "meeseeks.sock")
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `programs:
  - name: "test-program1"
    command: "echo"
    args: ["hello"]
`

	if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	s, err := New(sockPath, configFile, logger.New(), time.Millisecond)
	if err != nil {
		t.Fatalf("creating server failed: %s", err.Error())
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err = s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() unexpected error = %v", err)
	}

	t.Cleanup(func() {
		s.Stop()
		os.RemoveAll(tmpDir)
	})
}

func TestServer_HTTPHandlers(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "meeseeks.sock")
	// Clean up any existing socket
	os.Remove(sockPath)
	defer os.Remove(sockPath)

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
	defer cancel()

	err = s.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer s.Stop()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	client := NewClient(sockPath)

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
			name: "stop command",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFn(t, client)
		})
	}
}

func TestClient_SendRequest(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `programs:
  - name: "test-program"
    command: "echo"
    args: ["hello"]
    interval: 1s
`

	if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	d, err := New(sockPath, configFile, logger.New(), time.Millisecond)
	if err != nil {
		t.Fatalf("creating server failed: %s", err.Error())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err = d.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer d.Stop()

	// Create client
	client := NewClient(sockPath)

	tests := []struct {
		name   string
		testFn func(t *testing.T, client *Client)
	}{
		{
			name: "statistics request",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Statistics("")
				if err != nil {
					t.Fatalf("Statistics() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Fatalf("Statistics() expected success=true, got %v", resp.Success)
				}
			},
		},
		{
			name: "statistics specific program",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Statistics("test-program")
				if err != nil {
					t.Fatalf("Statistics() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Fatalf("Statistics() expected success=true, got %v", resp.Success)
				}
			},
		},
		{
			name: "logs request",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Logs("test-program")
				if err != nil {
					t.Fatalf("Logs() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Fatalf("Logs() expected success=true, got %v", resp.Success)
				}
			},
		},
		{
			name: "stop request",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Stop("test-program", "5s")
				if err != nil {
					t.Fatalf("Stop() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Fatalf("Stop() expected success=true, got %v", resp.Success)
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

func TestClient_ConnectToNonExistentDaemon(t *testing.T) {
	t.Parallel()
	client := NewClient("/nonexistent/path.sock")

	_, err := client.Statistics("")
	if err == nil {
		t.Fatalf("Expected error when connecting to non-existent daemon")
	}

	expectedMsg := "meeseeks server not running (socket not found)"
	if !containsString(err.Error(), expectedMsg) {
		t.Fatalf("Error should contain %q, got %q", expectedMsg, err.Error())
	}
}

func TestServer_IntegrationWithRealSocket(t *testing.T) {
	t.Parallel()
	tmpDir := filepath.Join("/tmp/", t.Name())
	err := os.MkdirAll(tmpDir, 0750)
	if err != nil {
		t.Fatalf("error creating temp folder %s", err.Error())
	}
	sockPath := filepath.Join(tmpDir, "meeseeks.sock")
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `programs:
  - name: "integration-test"
    command: "echo"
    args: ["integration"]
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
	defer func() {
		s.Stop()
		os.RemoveAll(tmpDir)
	}()

	// Give programs time to execute
	time.Sleep(200 * time.Millisecond)

	// Test client communication
	client := NewClient(sockPath)

	// Test statistics
	resp, err := client.Statistics("")
	if err != nil {
		t.Fatalf("Client Statistics() error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Statistics() expected success=true, got %v", resp.Success)
	}

	// Test logs
	resp, err = client.Logs("integration-test")
	if err != nil {
		t.Fatalf("Client Logs() error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Logs() expected success=true, got %v", resp.Success)
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
			client := NewClient(sockPath)
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

// Helper functions

func containsString(s, substr string) bool {
	return len(s) >= len(substr) &&
		(len(substr) == 0 ||
			func() bool {
				for i := 0; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}())
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

	client := NewClient(sockPath)

	b.ResetTimer()
	for range b.N {
		_, _ = client.Statistics("")
	}
}
