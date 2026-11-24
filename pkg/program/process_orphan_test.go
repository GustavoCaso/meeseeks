//go:build !windows

package program

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// startGroupLeader runs a script as the leader of a new process group, the way
// programs run, and reaps it so an exit is observable.
func startGroupLeader(t *testing.T, script string) (ProcessIdentity, <-chan struct{}) {
	t.Helper()

	// Not tied to a context, because the code under test must stop it.
	cmd := exec.Command("sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-exited
	})

	startTime, err := processStartTime(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("processStartTime() error = %v", err)
	}
	return ProcessIdentity{
		PID:       cmd.Process.Pid,
		StartTime: startTime,
		KillGrace: time.Second,
	}, exited
}

func waitForExit(t *testing.T, exited <-chan struct{}) {
	t.Helper()

	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("process still running")
	}
}

func groupIsGone(pgid int) bool {
	return errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}

func TestTerminateOrphan_StopsTheWholeProcessGroup(t *testing.T) {
	t.Parallel()

	id, exited := startGroupLeader(t, "sleep 30 & wait")

	if err := TerminateOrphan(id); err != nil {
		t.Fatalf("TerminateOrphan() error = %v", err)
	}

	waitForExit(t, exited)
	deadline := time.Now().Add(2 * time.Second)
	for !groupIsGone(id.PID) {
		if time.Now().After(deadline) {
			t.Fatal("a descendant survived")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTerminateOrphan_KillsProcessThatIgnoresSIGTERM(t *testing.T) {
	t.Parallel()

	id, exited := startGroupLeader(t, `trap "" TERM; while :; do sleep 1; done`)
	id.KillGrace = 200 * time.Millisecond
	// Give the shell time to install its trap.
	time.Sleep(100 * time.Millisecond)

	if err := TerminateOrphan(id); err != nil {
		t.Fatalf("TerminateOrphan() error = %v", err)
	}

	waitForExit(t, exited)
}

func TestTerminateOrphan_LeavesProcessThatReusedThePID(t *testing.T) {
	t.Parallel()

	id, exited := startGroupLeader(t, "sleep 30")
	id.StartTime++

	if err := TerminateOrphan(id); err != nil {
		t.Fatalf("TerminateOrphan() error = %v", err)
	}

	select {
	case <-exited:
		t.Fatal("an unrelated process with a reused PID was stopped")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestTerminateOrphan_RefusesPIDsThatWouldSignalOtherProcesses(t *testing.T) {
	t.Parallel()

	for _, pid := range []int{-1, 0, 1, os.Getpid()} {
		startTime, _ := processStartTime(pid)
		err := TerminateOrphan(ProcessIdentity{PID: pid, StartTime: startTime})
		if err == nil {
			t.Fatalf("TerminateOrphan(pid=%d) succeeded, want refusal", pid)
		}
	}
}

func TestTerminateOrphan_IgnoresProcessAlreadyGone(t *testing.T) {
	t.Parallel()

	id, exited := startGroupLeader(t, "exit 0")
	waitForExit(t, exited)

	if err := TerminateOrphan(id); err != nil {
		t.Fatalf("TerminateOrphan() error = %v", err)
	}
}
