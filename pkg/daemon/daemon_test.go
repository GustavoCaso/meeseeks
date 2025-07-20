package daemon

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

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

func TestDaemon_HandleRequest(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")
	d := New(sockPath)

	// Add test programs
	prog1 := program.New("test-program1", "echo", program.Args("hello"))
	prog2 := program.New("test-program2", "echo", program.Args("world"))
	d.AddProgram(prog1)
	d.AddProgram(prog2)

	tests := []struct {
		name    string
		request Request
		wantErr bool
		checkFn func(t *testing.T, resp Response)
	}{
		{
			name: "status all programs",
			request: Request{
				Command: "status",
			},
			checkFn: func(t *testing.T, resp Response) {
				if !resp.Success {
					t.Errorf("Expected success=true, got %v", resp.Success)
				}
				if resp.Data == nil {
					t.Errorf("Expected data to be non-nil")
				}
			},
		},
		{
			name: "status specific program",
			request: Request{
				Command: "status",
				Args:    map[string]interface{}{"program": "test-program1"},
			},
			checkFn: func(t *testing.T, resp Response) {
				if !resp.Success {
					t.Errorf("Expected success=true, got %v", resp.Success)
				}
				if resp.Data == nil {
					t.Errorf("Expected data to be non-nil")
				}
			},
		},
		{
			name: "status nonexistent program",
			request: Request{
				Command: "status",
				Args:    map[string]interface{}{"program": "nonexistent"},
			},
			checkFn: func(t *testing.T, resp Response) {
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
			request: Request{
				Command: "logs",
				Args:    map[string]interface{}{"program": "test-program1"},
			},
			checkFn: func(t *testing.T, resp Response) {
				if !resp.Success {
					t.Errorf("Expected success=true, got %v", resp.Success)
				}
				if resp.Data == nil {
					t.Errorf("Expected data to be non-nil")
				}
			},
		},
		{
			name: "logs without program name",
			request: Request{
				Command: "logs",
			},
			checkFn: func(t *testing.T, resp Response) {
				if resp.Success {
					t.Errorf("Expected success=false, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Errorf("Expected error message to be non-empty")
				}
			},
		},
		{
			name: "logs nonexistent program",
			request: Request{
				Command: "logs",
				Args:    map[string]interface{}{"program": "nonexistent"},
			},
			checkFn: func(t *testing.T, resp Response) {
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
			request: Request{
				Command: "stop",
			},
			checkFn: func(t *testing.T, resp Response) {
				if resp.Success {
					t.Errorf("Expected success=false for unimplemented stop, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Errorf("Expected error message for unimplemented command")
				}
			},
		},
		{
			name: "unknown command",
			request: Request{
				Command: "unknown",
			},
			checkFn: func(t *testing.T, resp Response) {
				if resp.Success {
					t.Errorf("Expected success=false, got %v", resp.Success)
				}
				if resp.Error == "" {
					t.Errorf("Expected error message to be non-empty")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := d.handleRequest(tt.request)
			tt.checkFn(t, resp)
		})
	}
}

func TestClient_SendRequest(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Start daemon
	d := New(sockPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
			name: "status request",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Status("")
				if err != nil {
					t.Errorf("Status() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Errorf("Status() expected success=true, got %v", resp.Success)
				}
			},
		},
		{
			name: "status specific program",
			testFn: func(t *testing.T, client *Client) {
				resp, err := client.Status("test-program")
				if err != nil {
					t.Errorf("Status() unexpected error = %v", err)
					return
				}
				if !resp.Success {
					t.Errorf("Status() expected success=true, got %v", resp.Success)
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
				resp, err := client.Stop("")
				if err != nil {
					t.Errorf("Stop() unexpected error = %v", err)
					return
				}
				// Stop is not implemented yet, so expect failure
				if resp.Success {
					t.Errorf("Stop() expected success=false (unimplemented), got %v", resp.Success)
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

	_, err := client.Status("")
	if err == nil {
		t.Errorf("Expected error when connecting to non-existent daemon")
	}

	expectedMsg := "daemon not running"
	if !containsString(err.Error(), expectedMsg) {
		t.Errorf("Error should contain %q, got %q", expectedMsg, err.Error())
	}
}

func TestGetSocketPath(t *testing.T) {
	path := GetSocketPath()
	if path == "" {
		t.Errorf("GetSocketPath() returned empty string")
	}

	expectedSuffix := "meeseeks.sock"
	if !endsWith(path, expectedSuffix) {
		t.Errorf("GetSocketPath() = %q, expected to end with %q", path, expectedSuffix)
	}
}

func TestGetPidFile(t *testing.T) {
	path := GetPidFile()
	if path == "" {
		t.Errorf("GetPidFile() returned empty string")
	}

	expectedSuffix := "meeseeks.pid"
	if !endsWith(path, expectedSuffix) {
		t.Errorf("GetPidFile() = %q, expected to end with %q", path, expectedSuffix)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Test status
	resp, err := client.Status("")
	if err != nil {
		t.Fatalf("Client Status() error: %v", err)
	}
	if !resp.Success {
		t.Errorf("Status() expected success=true, got %v", resp.Success)
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

// Test concurrent client connections
func TestDaemon_ConcurrentConnections(t *testing.T) {
	// Unix sockets have a path length limit (~104-108 characters depending on OS).
	// Using t.TempDir() creates very long paths that exceed this limit and cause
	// "bind: invalid argument" errors. We use /tmp directly with short names instead.
	sockPath := "/tmp/meeseeks-test-concurrent.sock"
	// Clean up any existing socket
	os.Remove(sockPath)

	// Start daemon
	d := New(sockPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	for i := 0; i < numClients; i++ {
		go func(clientID int) {
			client := NewClient(sockPath)
			resp, err := client.Status("")
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
	for i := 0; i < numClients; i++ {
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

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// Benchmark daemon request handling
func BenchmarkDaemon_HandleRequest(b *testing.B) {
	d := New("/tmp/bench.sock")
	prog := program.New("bench-program", "echo", program.Args("benchmark"))
	d.AddProgram(prog)

	req := Request{Command: "status"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.handleRequest(req)
	}
}
