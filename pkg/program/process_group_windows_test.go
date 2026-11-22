//go:build windows

package program

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const (
	helperRoleEnv    = "MEESEEKS_TEST_HELPER_ROLE"
	helperPIDFileEnv = "MEESEEKS_TEST_HELPER_PID_FILE"
	helperExitEnv    = "MEESEEKS_TEST_HELPER_PARENT_EXITS"
	helperLifetime   = 2 * time.Minute
)

// TestWindowsHelperProcess is re-executed by the tests below. The parent role
// spawns a child that inherits its output handles, like a script's subprocess.
func TestWindowsHelperProcess(_ *testing.T) {
	switch os.Getenv(helperRoleEnv) {
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestWindowsHelperProcess$")
		child.Env = append(os.Environ(), helperRoleEnv+"=child")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		pid := strconv.Itoa(child.Process.Pid)
		if err := os.WriteFile(os.Getenv(helperPIDFileEnv), []byte(pid), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if os.Getenv(helperExitEnv) != "" {
			os.Exit(0)
		}
		time.Sleep(helperLifetime)
		os.Exit(0)
	case "child":
		time.Sleep(helperLifetime)
		os.Exit(0)
	}
}

func startWindowsTree(t *testing.T, name string, parentExits bool, opts ...Option) (
	Program, <-chan struct{}, windows.Handle,
) {
	t.Helper()

	pidFile := filepath.Join(t.TempDir(), "child.pid")
	env := []string{helperRoleEnv + "=parent", helperPIDFileEnv + "=" + pidFile}
	if parentExits {
		env = append(env, helperExitEnv+"=1")
	}

	options := append([]Option{
		Args("-test.run=^TestWindowsHelperProcess$"),
		Envs(env...),
		Async(),
	}, opts...)

	p := New(name, os.Args[0], options...)
	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	return p, done, openWindowsChild(t, pidFile)
}

func openWindowsChild(t *testing.T, pidFile string) windows.Handle {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, convErr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
			if convErr == nil && pid > 0 {
				handle, openErr := windows.OpenProcess(
					windows.SYNCHRONIZE|windows.PROCESS_TERMINATE,
					false,
					uint32(pid),
				)
				if openErr != nil {
					t.Fatalf("OpenProcess(%d) error = %v", pid, openErr)
				}
				t.Cleanup(func() {
					_ = windows.TerminateProcess(handle, 1)
					_ = windows.CloseHandle(handle)
				})
				return handle
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("pid file %s was not written", pidFile)
	return 0
}

func windowsProcessExited(t *testing.T, handle windows.Handle, wait time.Duration) bool {
	t.Helper()

	event, err := windows.WaitForSingleObject(handle, uint32(wait.Milliseconds()))
	if err != nil {
		t.Fatalf("WaitForSingleObject() error = %v", err)
	}
	return event == windows.WAIT_OBJECT_0
}

func waitWindowsDone(t *testing.T, done <-chan struct{}, limit time.Duration) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("program did not finish within %v", limit)
	}
}

func TestWindowsJob_DeadlineKillsDescendantHoldingOutput(t *testing.T) {
	t.Parallel()

	_, done, child := startWindowsTree(t, "deadline", false,
		Deadline(5*time.Second),
		KillGrace(200*time.Millisecond),
	)

	waitWindowsDone(t, done, 20*time.Second)
	if !windowsProcessExited(t, child, 5*time.Second) {
		t.Fatal("descendant is still running after the deadline")
	}
}

func TestWindowsJob_ShutdownKillsDescendant(t *testing.T) {
	t.Parallel()

	p, done, child := startWindowsTree(t, "shutdown", false)

	if err := p.Shutdown(200 * time.Millisecond); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	waitWindowsDone(t, done, 10*time.Second)
	if !windowsProcessExited(t, child, 5*time.Second) {
		t.Fatal("descendant is still running after Shutdown")
	}
}

func TestWindowsJob_NaturalExitKeepsBackgroundProcess(t *testing.T) {
	t.Parallel()

	p, done, child := startWindowsTree(t, "natural-exit", true, KillGrace(0))

	waitWindowsDone(t, done, waitDelayMargin+10*time.Second)

	if p.State() != StateFinished {
		t.Fatalf("State() = %s, want finished", StateToString[p.State()])
	}
	if windowsProcessExited(t, child, 0) {
		t.Fatal("background process should survive a natural exit")
	}
}
