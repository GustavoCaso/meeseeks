package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/pkg/program"
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

func TestServer_StartAbortsWhenOrphanCannotBeStopped(t *testing.T) {
	dir := shortTempDir(t)
	s, runs := newRecordingServer(t, dir)

	orphan := program.ProcessIdentity{PID: 4242, StartTime: 7}
	writeRegistry(t, s.processes.path, orphan)
	s.processes.terminate = func(program.ProcessIdentity) error {
		return errors.New("still running after SIGKILL")
	}

	if err := s.Start(t.Context()); err == nil {
		t.Fatal("Start() succeeded although a program from a crashed daemon is still running")
	}

	time.Sleep(100 * time.Millisecond)
	if got := countServerRuns(t, runs); got != 0 {
		t.Fatalf("program started %d times next to its orphan, want 0", got)
	}

	// The record is kept, so a later start retries the cleanup.
	if got := readRegistry(t, s.processes.path); len(got) != 1 || got[0] != orphan {
		t.Fatalf("registry = %v, want [%v]", got, orphan)
	}

	// The lock is released, so a later start can retry.
	lock, err := acquireInstanceLock(instanceLockPath(s.sockPath))
	if err != nil {
		t.Fatalf("instance lock still held after failed start: %v", err)
	}
	_ = lock.release()
}

func TestServer_StartDropsInvalidOrphanRecords(t *testing.T) {
	dir := shortTempDir(t)
	s, runs := newRecordingServer(t, dir)
	writeRegistry(t, s.processes.path, program.ProcessIdentity{PID: 1})

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v, an invalid record must not block startup", err)
	}
	waitForServerRun(t, runs)

	for _, id := range readRegistry(t, s.processes.path) {
		if id.PID == 1 {
			t.Fatal("invalid record for PID 1 was kept")
		}
	}
}

func TestServer_StartReplacesSocketLeftByCrash(t *testing.T) {
	dir := shortTempDir(t)
	s, runs := newRecordingServer(t, dir)

	// A crashed daemon leaves its socket file behind, refusing connections.
	var listenConfig net.ListenConfig
	stale, err := listenConfig.Listen(t.Context(), "unix", s.sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = stale.Close()

	if err = s.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForServerRun(t, runs)
}

func writeRegistry(t *testing.T, path string, ids ...program.ProcessIdentity) {
	t.Helper()

	data, err := json.Marshal(ids)
	if err != nil {
		t.Fatalf("encode registry: %v", err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}

func readRegistry(t *testing.T, path string) []program.ProcessIdentity {
	t.Helper()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var ids []program.ProcessIdentity
	if err = json.Unmarshal(data, &ids); err != nil {
		t.Fatalf("decode registry: %v", err)
	}
	return ids
}
