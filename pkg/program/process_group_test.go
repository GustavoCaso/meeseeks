//go:build !windows

package program

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// descendantCleanupScript starts a descendant whose TERM trap takes cleanup
// seconds and writes $MARKER. The leader then blocks, and dies on SIGTERM.
func descendantCleanupScript(cleanup, redirect string) string {
	return fmt.Sprintf(
		`bash -c 'trap "sleep %s; echo done > \"$MARKER\"; exit 0" TERM; `+
			`echo ready > "$READY"; while :; do sleep 0.05; done' %s & `+
			`while [ ! -f "$READY" ]; do sleep 0.02; done; echo $$ > "$LEADER"; sleep 300`,
		cleanup, redirect,
	)
}

func TestProcessGroup_DeadlineAfterLeaderExitKeepsSuccess(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	p := New("deadline-while-draining", "bash",
		Args("-c", `sleep 300 & echo $! > "$PID_FILE"; exit 0`),
		Envs("PID_FILE="+pidFile),
		Deadline(time.Second),
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
		t.Fatalf("State() = %s, want finished: %s", StateToString[p.State()], p.Stderr())
	}
	if processGone(pid) {
		t.Fatal("background process should survive a successful leader exit")
	}
}

func TestProcessGroup_DescendantKeepsGraceAfterLeaderExit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "cleanup")
	leader := filepath.Join(dir, "leader")

	p := New("descendant-grace", "bash",
		Args("-c", descendantCleanupScript("1", ">/dev/null 2>&1 </dev/null")),
		Envs("MARKER="+marker, "READY="+filepath.Join(dir, "ready"), "LEADER="+leader),
		KillGrace(5*time.Second),
		Async(),
	)

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	readPID(t, leader)
	time.Sleep(200 * time.Millisecond)

	if err = p.Shutdown(5 * time.Second); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	waitDone(t, done, 8*time.Second)

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("descendant cleanup was cut short: %v", statErr)
	}
}

func TestProcessGroup_ConcurrentShutdownHonorsExtendedDeadline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "cleanup")
	ready := filepath.Join(dir, "ready")

	p := New("concurrent-shutdown", "bash",
		Args(
			"-c",
			`trap 'sleep 3; echo done > "$MARKER"; exit 0' TERM; echo $$ > "$READY"; sleep 300; exit 0`,
		),
		Envs("MARKER="+marker, "READY="+ready),
		Async(),
	)

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	readPID(t, ready)
	time.Sleep(200 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Go(func() { _ = p.Shutdown(time.Second) })
	time.Sleep(100 * time.Millisecond)
	wg.Go(func() { _ = p.Shutdown(5 * time.Second) })
	wg.Wait()

	waitDone(t, done, 6*time.Second)

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("earlier Shutdown ignored the extended deadline: %v", statErr)
	}
}

func TestProcessGroup_ShutdownTimeoutCoversInheritedOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "cleanup")
	leader := filepath.Join(dir, "leader")

	p := New("shutdown-drain", "bash",
		Args("-c", descendantCleanupScript("6", "")),
		Envs("MARKER="+marker, "READY="+filepath.Join(dir, "ready"), "LEADER="+leader),
		KillGrace(0),
		Async(),
	)

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	readPID(t, leader)
	time.Sleep(200 * time.Millisecond)

	if err = p.Shutdown(30 * time.Second); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	waitDone(t, done, 12*time.Second)

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("descendant was killed before the granted timeout: %v", statErr)
	}
}

func TestProcessGroup_UnreapedLeaderReservesGroup(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	cmd := exec.Command("bash", "-c", `sleep 300 & echo $! > "$PID_FILE"; exit 0`)
	cmd.Env = append(os.Environ(), "PID_FILE="+pidFile)
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	leader := cmd.Process.Pid
	child := readPID(t, pidFile)

	if !waitLeaderExit(leader) {
		_ = cmd.Wait()
		t.Skip("platform cannot observe exit without reaping")
	}
	defer func() { _ = cmd.Wait() }()

	if !hasLiveDescendant(leader) {
		t.Fatal("live descendant was not detected")
	}

	if err := syscall.Kill(child, syscall.SIGKILL); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for hasLiveDescendant(leader) {
		if time.Now().After(deadline) {
			t.Fatal("group of only a zombie leader counts as live")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The zombie must still exist, because it is what reserves the group ID.
	if err := syscall.Kill(leader, 0); err != nil {
		t.Fatalf("leader was reaped early: %v", err)
	}
}

// slowWriter blocks inside Write until released.
type slowWriter struct {
	entered  chan struct{}
	release  chan struct{}
	finished atomic.Int32
	once     sync.Once
}

func (w *slowWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	w.finished.Add(1)
	return len(data), nil
}

func TestPipes_DrainWaitsForInFlightWrite(t *testing.T) {
	t.Parallel()

	outReader, outWriter := io.Pipe()
	errReader, errWriter := io.Pipe()
	inReader, inWriter := io.Pipe()
	p := &pipes{
		outReader: outReader, outWriter: outWriter,
		errReader: errReader, errWriter: errWriter,
		inReader: inReader, inWriter: inWriter,
	}

	writer := &slowWriter{entered: make(chan struct{}), release: make(chan struct{})}
	if err := p.connect(
		exec.Command("true"),
		writer,
		io.Discard,
		strings.NewReader(""),
	); err != nil {
		t.Fatalf("connect() error = %v", err)
	}
	if _, err := p.childEnds[0].WriteString("data"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	<-writer.entered

	returned := make(chan bool, 1)
	go func() { returned <- p.drain(10*time.Millisecond, nil) }()

	select {
	case <-returned:
		t.Fatal("drain returned during an in-flight write")
	case <-time.After(200 * time.Millisecond):
	}

	close(writer.release)
	if drained := <-returned; drained {
		t.Fatal("drain reported complete output")
	}
	if writer.finished.Load() != 1 {
		t.Fatal("in-flight write did not finish before drain returned")
	}
	if _, err := p.outGate.Write([]byte("late")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("late Write() error = %v, want %v", err, io.ErrClosedPipe)
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
