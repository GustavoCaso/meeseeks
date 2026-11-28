//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func recordedPIDs(t *testing.T, path string) []int {
	t.Helper()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read PIDs: %v", err)
	}

	var pids []int
	for field := range strings.FieldsSeq(string(data)) {
		pid, convErr := strconv.Atoi(field)
		if convErr != nil {
			t.Fatalf("parse PID %q: %v", field, convErr)
		}
		pids = append(pids, pid)
	}
	return pids
}

func TestStart_StopsProgramsLeftByCrashedDaemon(t *testing.T) {
	configDir := setMeeseeksConfigDirForTest(t)
	pidsFile := filepath.Join(configDir, "pids")
	writeDaemonConfig(t, configDir, fmt.Sprintf(
		"programs:\n  - name: job\n    command: sh\n    args: [\"-c\", %q, \"sh\", %q]\n",
		`echo $$ >> "$1"; exec sleep 30`,
		pidsFile,
	))
	bin := buildMeeseeks(t)

	crashed := startForegroundDaemon(t, bin)
	waitFor(t, 5*time.Second, func() bool {
		return len(recordedPIDs(t, pidsFile)) == 1
	}, "program never started")
	if err := crashed.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill daemon: %v", err)
	}
	<-crashed.exited

	orphan := recordedPIDs(t, pidsFile)[0]
	if err := syscall.Kill(orphan, 0); err != nil {
		t.Fatalf("expected the program to outlive its daemon: %v", err)
	}

	startForegroundDaemon(t, bin)
	waitFor(t, 5*time.Second, func() bool {
		return len(recordedPIDs(t, pidsFile)) == 2
	}, "restarted daemon never relaunched the program")

	waitFor(t, 3*time.Second, func() bool {
		return errors.Is(syscall.Kill(orphan, 0), syscall.ESRCH)
	}, "program from the crashed daemon is still running")
}
