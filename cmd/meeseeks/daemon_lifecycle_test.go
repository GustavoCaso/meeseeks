package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const longRunningConfig = `programs:
  - name: job
    command: sleep
    args: ["30"]
`

func buildMeeseeks(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "meeseeks")
	if runtime.GOOS == "windows" {
		// Windows runs a file only with an executable extension.
		bin += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build meeseeks: %v\n%s", err, output)
	}
	return bin
}

func writeDaemonConfig(t *testing.T, configDir, content string) {
	t.Helper()

	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// runMeeseeks runs one CLI command and returns its exit code and output.
func runMeeseeks(t *testing.T, bin string, args ...string) (int, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var output bytes.Buffer
	command := exec.CommandContext(ctx, bin, args...)
	command.Env = os.Environ()
	command.Stdout = &output
	command.Stderr = &output
	// A detached daemon inherits nothing from this command's output pipes, but
	// never let a stray holder keep the test waiting.
	command.WaitDelay = time.Second

	err := command.Run()
	if err == nil {
		return 0, output.String()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), output.String()
	}
	t.Fatalf("run meeseeks %v: %v", args, err)
	return 0, ""
}

// foregroundDaemon is a daemon started the way a service manager starts it.
type foregroundDaemon struct {
	cmd    *exec.Cmd
	exited chan struct{}
}

func startForegroundDaemon(t *testing.T, bin string) *foregroundDaemon {
	t.Helper()

	// Not tied to the test context, so cleanup can stop the daemon gracefully.
	cmd := exec.Command(bin, "start")
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}

	daemon := &foregroundDaemon{cmd: cmd, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(daemon.exited)
	}()

	t.Cleanup(func() {
		// Windows cannot deliver SIGTERM, so the daemon is killed at once there.
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-daemon.exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
	})

	waitFor(t, 10*time.Second, func() bool {
		_, err := os.Stat(getSocketPath())
		return err == nil
	}, "daemon socket never appeared")

	return daemon
}

func TestExitCommand_StopsDaemonStartedInForeground(t *testing.T) {
	configDir := setMeeseeksConfigDirForTest(t)
	writeDaemonConfig(t, configDir, longRunningConfig)
	bin := buildMeeseeks(t)
	daemon := startForegroundDaemon(t, bin)

	code, output := runMeeseeks(t, bin, "exit")
	if code != 0 {
		t.Fatalf("exit returned %d: %s", code, strings.TrimSpace(output))
	}

	select {
	case <-daemon.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon still running after exit")
	}
}

func TestStartDetached_ReportsInvalidConfig(t *testing.T) {
	configDir := setMeeseeksConfigDirForTest(t)
	writeDaemonConfig(t, configDir, "programs: []\n")
	bin := buildMeeseeks(t)

	code, output := runMeeseeks(t, bin, "start", "-d")

	if code == 0 {
		t.Fatalf("start -d succeeded with an invalid config: %s", output)
	}
	if !strings.Contains(output, "no programs defined") {
		t.Fatalf("output does not explain the failure: %q", output)
	}
	if _, err := os.Stat(getPidFile()); !os.IsNotExist(err) {
		t.Fatalf("PID file left behind after failed start, stat error = %v", err)
	}
}

func TestStartDetached_DoesNotReplaceRunningDaemon(t *testing.T) {
	configDir := setMeeseeksConfigDirForTest(t)
	writeDaemonConfig(t, configDir, longRunningConfig)
	bin := buildMeeseeks(t)
	daemon := startForegroundDaemon(t, bin)
	waitFor(t, 5*time.Second, func() bool {
		_, err := os.Stat(getPidFile())
		return err == nil
	}, "daemon never wrote its PID file")

	code, output := runMeeseeks(t, bin, "start", "-d")

	if code == 0 {
		t.Fatalf("start -d succeeded while a daemon was running: %s", output)
	}
	if !strings.Contains(output, "already running") {
		t.Fatalf("output does not explain the failure: %q", output)
	}

	pid, err := os.ReadFile(getPidFile())
	if err != nil {
		t.Fatalf("read PID file: %v", err)
	}
	if want := strconv.Itoa(daemon.cmd.Process.Pid); string(pid) != want {
		t.Fatalf("PID file = %s, want running daemon %s", pid, want)
	}
}

func TestStartDetached_WorksRightAfterExit(t *testing.T) {
	configDir := setMeeseeksConfigDirForTest(t)
	writeDaemonConfig(t, configDir, longRunningConfig)
	bin := buildMeeseeks(t)

	if code, output := runMeeseeks(t, bin, "start", "-d"); code != 0 {
		t.Fatalf("first start -d returned %d: %s", code, output)
	}
	if code, output := runMeeseeks(t, bin, "exit"); code != 0 {
		t.Fatalf("exit returned %d: %s", code, output)
	}

	if code, output := runMeeseeks(t, bin, "start", "-d"); code != 0 {
		t.Fatalf("second start -d returned %d: %s", code, output)
	}
	if code, output := runMeeseeks(t, bin, "exit"); code != 0 {
		t.Fatalf("final exit returned %d: %s", code, output)
	}
}
