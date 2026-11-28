//go:build !windows

package meeseeks

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/program"
)

func TestMeeseekShutdown_WaitsForRunningCallback(t *testing.T) {
	t.Parallel()

	pidFile := filepath.Join(t.TempDir(), "callback.pid")
	m := New()
	prog := program.New(
		"job",
		"true",
		program.OnSuccess(&program.Callback{
			Command: "sh",
			Args:    []string{"-c", `echo $$ > "$1"; exec sleep 30`, "sh", pidFile},
		}),
	)
	if err := m.AddProgram(prog); err != nil {
		t.Fatalf("AddProgram() error = %v", err)
	}

	m.Start(t.Context())
	pid := waitForPIDFile(t, pidFile)

	started := time.Now()
	_ = m.Shutdown(200 * time.Millisecond)
	if elapsed := time.Since(started); elapsed > 15*time.Second {
		t.Fatalf("Shutdown took %s, the callback was not stopped", elapsed)
	}

	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("callback %d still running after Shutdown returned (kill: %v)", pid, err)
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no PID written to %s", path)
	return 0
}
