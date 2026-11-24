package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

// shortTempDir avoids t.TempDir, whose long test-named paths can exceed the
// Unix socket path limit of 104 bytes on macOS.
func shortTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "mks") //nolint:usetesting // t.TempDir paths are too long
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// newRecordingServer returns a server whose program appends to the returned
// file each time it starts, then stays running.
func newRecordingServer(t *testing.T, dir string) (*Server, string) {
	t.Helper()

	runs := filepath.Join(dir, "runs")
	config := filepath.Join(dir, "config.yaml")
	content := fmt.Sprintf(
		"programs:\n  - name: job\n    command: sh\n    args: [\"-c\", %q, \"sh\", %q]\n",
		`echo run >> "$1"; exec sleep 30`,
		runs,
	)
	if err := os.WriteFile(config, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	s, err := New(filepath.Join(dir, "meeseeks.sock"), config, logger.New(), 2*time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	return s, runs
}

func countServerRuns(t *testing.T, runs string) int {
	t.Helper()

	data, err := os.ReadFile(runs)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("read runs file: %v", err)
	}
	return strings.Count(string(data), "run\n")
}

func waitForServerRun(t *testing.T, runs string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if countServerRuns(t, runs) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("program never started")
}

func TestServer_SecondInstanceDoesNotTakeOverRunningDaemon(t *testing.T) {
	dir := shortTempDir(t)
	first, runs := newRecordingServer(t, dir)
	if err := first.Start(t.Context()); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	waitForServerRun(t, runs)

	second, _ := newRecordingServer(t, dir)
	err := second.Start(t.Context())
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Start() error = %v, want %v", err, ErrAlreadyRunning)
	}

	resp, err := NewClient(first.sockPath).Statistics(t.Context(), "job")
	if err != nil {
		t.Fatalf("first daemon unreachable after second start: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Statistics() error = %s", resp.Error)
	}

	time.Sleep(100 * time.Millisecond)
	if got := countServerRuns(t, runs); got != 1 {
		t.Fatalf("program started %d times, want 1", got)
	}
}

func TestServer_CanStartAfterPreviousInstanceStopped(t *testing.T) {
	dir := shortTempDir(t)
	first, runs := newRecordingServer(t, dir)
	if err := first.Start(t.Context()); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	waitForServerRun(t, runs)
	if err := first.Stop(); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}

	second, _ := newRecordingServer(t, dir)
	if err := second.Start(t.Context()); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
}

// startServerWithShortTimeouts runs a server whose HTTP timeouts expire well
// before the configured program finishes, standing in for the real 10s and 15s
// limits.
func startServerWithShortTimeouts(t *testing.T, programConfig string) *Server {
	t.Helper()

	dir := shortTempDir(t)
	config := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(config, []byte("programs:\n"+programConfig), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	s, err := New(filepath.Join(dir, "meeseeks.sock"), config, logger.New(), 2*time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	s.server.ReadTimeout = 200 * time.Millisecond
	s.server.WriteTimeout = 200 * time.Millisecond

	if err = s.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	return s
}

func TestServer_SynchronousRunOutlastsHTTPTimeouts(t *testing.T) {
	s := startServerWithShortTimeouts(t, `  - name: slow
    command: sleep
    args: ["0.8"]
    initial_delay: 1h
`)

	resp, err := NewClient(s.sockPath).RunProgram(t.Context(), "slow", false)
	if err != nil {
		t.Fatalf("RunProgram() error = %v", err)
	}
	if !resp.Success {
		t.Fatalf("RunProgram() failed: %s", resp.Error)
	}
}
