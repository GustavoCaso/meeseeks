package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/program"
)

func TestDaemon_StartStop(t *testing.T) {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sockPath := tt.setup()
			d := New(sockPath)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			err := d.Start(ctx)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Start() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Start() unexpected error = %v", err)
				return
			}

			// Verify socket file exists
			if _, err := os.Stat(sockPath); os.IsNotExist(err) {
				t.Errorf("Socket file was not created at %s", sockPath)
			}

			// Test double start (should return error)
			err = d.Start(ctx)
			if err == nil {
				t.Errorf("Second Start() should return error but got none")
			}

			// Stop daemon
			err = d.Stop()
			if err != nil {
				t.Errorf("Stop() unexpected error = %v", err)
			}

			// Verify socket file is removed
			if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
				t.Errorf("Socket file should be removed after Stop()")
			}

			// Test double stop (should not error)
			err = d.Stop()
			if err != nil {
				t.Errorf("Second Stop() unexpected error = %v", err)
			}
		})
	}
}

func TestDaemon_AddProgramAndStart(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")
	d := New(sockPath)

	// Add a test program
	prog := program.New("test-program", "echo", program.Args("hello"))
	err := d.AddProgram(prog)
	if err != nil {
		t.Fatalf("AddProgram() unexpected error = %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err = d.Start(ctx)
	if err != nil {
		t.Fatalf("Start() unexpected error = %v", err)
	}
	defer d.Stop()

	// Start programs
	d.StartPrograms(ctx)

	// Give programs time to start
	time.Sleep(100 * time.Millisecond)
}

func TestServer_HTTPHandlers(t *testing.T) {
	// Unix sockets have a path length limit (~104-108 characters depending on OS).
	// Using t.TempDir() creates very long paths that exceed this limit and cause
	// "bind: invalid argument" errors. We use /tmp directly with short names instead.
	sockPath := "/tmp/meeseeks-test-handlers.sock"
	// Clean up any existing socket
	os.Remove(sockPath)
	defer os.Remove(sockPath)

	s := New(sockPath)

	// Add test programs
	prog1 := program.New("test-program1", "echo", program.Args("hello"))
	prog2 := program.New("test-program2", "echo", program.Args("world"))
	s.AddProgram(prog1)
	s.AddProgram(prog2)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	err := s.Start(ctx)
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
					t.Errorf("Statistics() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Errorf("Expected success=true, got %v", resp.Success)
				}
				if resp.Data == nil {
					t.Errorf("Expected data to be non-nil")
				}
			},
		},
		{
			name: "statistics specific program",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Statistics("test-program1")
				if err != nil {
					t.Errorf("Statistics() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Errorf("Expected success=true, got %v", resp.Success)
				}
				if resp.Data == nil {
					t.Errorf("Expected data to be non-nil")
				}
			},
		},
		{
			name: "statistics nonexistent program",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Statistics("nonexistent")
				if err != nil {
					t.Errorf("Statistics() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Errorf("Expected success=false, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Errorf("Expected error message to be non-empty")
				}
			},
		},
		{
			name: "logs with program name",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Logs("test-program1")
				if err != nil {
					t.Errorf("Logs() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Errorf("Expected success=true, got %v", resp.Success)
				}
				if resp.Data == nil {
					t.Errorf("Expected data to be non-nil")
				}
			},
		},
		{
			name: "logs nonexistent program",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Logs("nonexistent")
				if err != nil {
					t.Errorf("Logs() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Errorf("Expected success=false, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Errorf("Expected error message to be non-empty")
				}
			},
		},
		{
			name: "stop command",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Stop("", "5s")
				if err != nil {
					t.Errorf("Stop() unexpected error = %v", err)
					return
				}
				if resp.Success {
					t.Errorf("Expected success=false for empty program name, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Errorf("Expected error message for empty program name")
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

	// Start daemon
	d := New(sockPath)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err := d.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}
	defer d.Stop()

	// Add test program
	prog := program.New("test-program", "echo", program.Args("hello"))
	d.AddProgram(prog)

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
					t.Errorf("Statistics() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Errorf("Statistics() expected success=true, got %v", resp.Success)
				}
			},
		},
		{
			name: "statistics specific program",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Statistics("test-program")
				if err != nil {
					t.Errorf("Statistics() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Errorf("Statistics() expected success=true, got %v", resp.Success)
				}
			},
		},
		{
			name: "logs request",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Logs("test-program")
				if err != nil {
					t.Errorf("Logs() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Errorf("Logs() expected success=true, got %v", resp.Success)
				}
			},
		},
		{
			name: "stop request",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Stop("test-program", "5s")
				if err != nil {
					t.Errorf("Stop() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Errorf("Stop() expected success=true, got %v", resp.Success)
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
	client := NewClient("/nonexistent/path.sock")

	_, err := client.Statistics("")
	if err == nil {
		t.Errorf("Expected error when connecting to non-existent daemon")
	}

	expectedMsg := "meeseeks server not running (socket not found)"
	if !containsString(err.Error(), expectedMsg) {
		t.Errorf("Error should contain %q, got %q", expectedMsg, err.Error())
	}
}

func TestDaemon_IntegrationWithRealSocket(t *testing.T) {
	// Unix sockets have a path length limit (~104-108 characters depending on OS).
	// Using t.TempDir() creates very long paths that exceed this limit and cause
	// "bind: invalid argument" errors. We use /tmp directly with short names instead.
	sockPath := "/tmp/meeseeks-test-integration.sock"
	// Clean up any existing socket
	os.Remove(sockPath)

	// Start daemon
	d := New(sockPath)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	err := d.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}
	defer func() {
		d.Stop()
		os.Remove(sockPath) // Ensure cleanup of socket file
	}()

	// Add and start programs
	prog := program.New("integration-test", "echo", program.Args("integration"))
	err = d.AddProgram(prog)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}

	d.StartPrograms(ctx)

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
		t.Errorf("Statistics() expected success=true, got %v", resp.Success)
	}

	// Test logs
	resp, err = client.Logs("integration-test")
	if err != nil {
		t.Fatalf("Client Logs() error: %v", err)
	}
	if !resp.Success {
		t.Errorf("Logs() expected success=true, got %v", resp.Success)
	}
}

// Test concurrent client connections.
func TestDaemon_ConcurrentConnections(t *testing.T) {
	// Unix sockets have a path length limit (~104-108 characters depending on OS).
	// Using t.TempDir() creates very long paths that exceed this limit and cause
	// "bind: invalid argument" errors. We use /tmp directly with short names instead.
	sockPath := "/tmp/meeseeks-test-concurrent.sock"
	// Clean up any existing socket
	os.Remove(sockPath)

	// Start daemon
	d := New(sockPath)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	err := d.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}
	defer func() {
		d.Stop()
		os.Remove(sockPath) // Ensure cleanup of socket file
	}()

	// Add test program
	prog := program.New("concurrent-test", "echo", program.Args("concurrent"))
	d.AddProgram(prog)

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
			t.Errorf("Client %d failed: %v", i, err)
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
	// Unix sockets have a path length limit (~104-108 characters depending on OS).
	sockPath := "/tmp/bench.sock"
	os.Remove(sockPath)
	defer os.Remove(sockPath)

	s := New(sockPath)
	prog := program.New("bench-program", "echo", program.Args("benchmark"))
	s.AddProgram(prog)

	ctx, cancel := context.WithTimeout(b.Context(), 30*time.Second)
	defer cancel()

	err := s.Start(ctx)
	if err != nil {
		b.Fatalf("Failed to start server: %v", err)
	}
	defer s.Stop()

	time.Sleep(100 * time.Millisecond) // Give server time to start

	client := NewClient(sockPath)

	b.ResetTimer()
	for range b.N {
		_, _ = client.Statistics("")
	}
}
