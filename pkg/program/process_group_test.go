//go:build !windows

package program

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func readPID(t *testing.T, path string) int {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			value := strings.TrimSpace(string(data))
			if pid, convErr := strconv.Atoi(value); convErr == nil && pid > 0 {
				t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("pid file %s was not written", path)
	return 0
}

func processGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func assertProcessGone(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if processGone(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("descendant process %d is still running", pid)
}

func waitDone(t *testing.T, done <-chan struct{}, limit time.Duration) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("program did not finish within %v", limit)
	}
}

func TestProcessGroup_DeadlineKillsDescendantHoldingStderr(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	p := New("descendant", "bash",
		Args("-c", `sleep 300 & echo $! > "$PID_FILE"; wait`),
		Envs("PID_FILE="+pidFile),
		Deadline(300*time.Millisecond),
		KillGrace(500*time.Millisecond),
		Async(),
	)

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pid := readPID(t, pidFile)

	waitDone(t, done, 3*time.Second)
	assertProcessGone(t, pid)
}

func TestProcessGroup_DeadlineRunsTermTrapWithinGrace(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "cleanup")

	// sleep runs in the foreground: bash defers the trap until it exits, so
	// the trap only runs if SIGTERM reaches the whole group.
	p := New("trap", "bash",
		Args("-c", `trap 'sleep 0.2; echo done > "$MARKER"; exit 0' TERM; sleep 300; exit 0`),
		Envs("MARKER="+marker),
		Deadline(300*time.Millisecond),
		KillGrace(3*time.Second),
		Async(),
	)

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitDone(t, done, 2*time.Second)

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("TERM trap did not complete: %v", statErr)
	}
}

func TestProcessGroup_DeadlineKillsDescendantIgnoringTerm(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	p := New("ignore-term", "bash",
		Args(
			"-c",
			`bash -c 'trap "" TERM; echo $$ > "$PID_FILE"; while :; do sleep 0.1; done' & wait`,
		),
		Envs("PID_FILE="+pidFile),
		Deadline(300*time.Millisecond),
		KillGrace(300*time.Millisecond),
		Async(),
	)

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pid := readPID(t, pidFile)

	waitDone(t, done, 3*time.Second)
	assertProcessGone(t, pid)
}

func TestProcessGroup_ShutdownKillsDescendants(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	p := New("shutdown", "bash",
		Args("-c", `sleep 300 & echo $! > "$PID_FILE"; wait`),
		Envs("PID_FILE="+pidFile),
		Async(),
	)

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pid := readPID(t, pidFile)

	if err = p.Shutdown(time.Second); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	waitDone(t, done, 2*time.Second)
	assertProcessGone(t, pid)
}

func TestProcessGroup_ShutdownHonorsLongerKillGrace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "cleanup")
	ready := filepath.Join(dir, "ready")

	p := New("shutdown-grace", "bash",
		Args(
			"-c",
			`trap 'sleep 0.5; echo done > "$MARKER"; exit 0' TERM; echo $$ > "$READY"; sleep 300; exit 0`,
		),
		Envs("MARKER="+marker, "READY="+ready),
		KillGrace(3*time.Second),
		Async(),
	)

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	readPID(t, ready)
	// A SIGTERM landing while bash forks its foreground command can be lost.
	time.Sleep(200 * time.Millisecond)

	if err = p.Shutdown(50 * time.Millisecond); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	waitDone(t, done, 2*time.Second)

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("TERM trap was cut short: %v", statErr)
	}
}

func TestProcessGroup_ShutdownExtendsPendingKill(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "cleanup")
	ready := filepath.Join(dir, "ready")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := New("extend-kill", "bash",
		Args(
			"-c",
			`trap 'sleep 1; echo done > "$MARKER"; exit 0' TERM; echo $$ > "$READY"; sleep 300; exit 0`,
		),
		Envs("MARKER="+marker, "READY="+ready),
		KillGrace(200*time.Millisecond),
		Async(),
	)

	done, err := p.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	readPID(t, ready)
	// A SIGTERM landing while bash forks its foreground command can be lost.
	time.Sleep(200 * time.Millisecond)

	// Reload and daemon shutdown cancel the context before calling Shutdown.
	cancel()
	time.Sleep(50 * time.Millisecond)

	if err = p.Shutdown(5 * time.Second); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	waitDone(t, done, 3*time.Second)

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("pending SIGKILL was not extended: %v", statErr)
	}
}

func TestProcessGroup_DeadlineWithCleanExitIsAnError(t *testing.T) {
	t.Parallel()

	p := New("deadline-clean-exit", "bash",
		Args("-c", `trap 'exit 0' TERM; sleep 300; exit 0`),
		Deadline(300*time.Millisecond),
		KillGrace(2*time.Second),
		Async(),
	)

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitDone(t, done, 2*time.Second)

	if p.State() != StateError {
		t.Fatalf("State() = %s, want error", StateToString[p.State()])
	}
	if !strings.Contains(p.Stderr(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Stderr() = %q, want deadline error", p.Stderr())
	}
}

func TestProcessGroup_NaturalExitDoesNotHangOnInheritedPipe(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	p := New("natural-exit", "bash",
		Args("-c", `sleep 300 & echo $! > "$PID_FILE"; exit 0`),
		Envs("PID_FILE="+pidFile),
		KillGrace(0),
		Async(),
	)

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pid := readPID(t, pidFile)

	waitDone(t, done, waitDelayMargin+3*time.Second)

	if p.State() != StateFinished {
		t.Fatalf("State() = %s, want finished", StateToString[p.State()])
	}
	if processGone(pid) {
		t.Fatal("background process should survive a natural exit")
	}
}
