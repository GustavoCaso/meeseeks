//go:build windows

package program

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// jobTerminatedExitCode is the exit code of processes killed through the job.
const jobTerminatedExitCode = 1

// platformGroup tracks a program's process tree with a Job Object. The job is
// deliberately not KILL_ON_JOB_CLOSE, so background processes survive a natural
// exit as they do on Unix.
type platformGroup struct {
	process *os.Process
	job     windows.Handle
}

// setProcessGroup starts the command in a new console process group so it can
// receive CTRL_BREAK without affecting meeseeks.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// attach assigns the started process to a new job. Go offers no way to start a
// process suspended and resume it, so a child spawned before this call escapes.
func (g *platformGroup) attach(process *os.Process) error {
	g.process = process

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create job object: %w", err)
	}

	handle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(process.Pid), //nolint:gosec // Windows process IDs are DWORD values
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("open process: %w", err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	if err = windows.AssignProcessToJobObject(job, handle); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("assign process to job: %w", err)
	}

	g.job = job
	return nil
}

// terminate sends CTRL_BREAK when possible. Without a shared console, such as
// under the detached daemon, no graceful signal exists, so it kills the tree.
func (g *platformGroup) terminate() error {
	err := windows.GenerateConsoleCtrlEvent(
		windows.CTRL_BREAK_EVENT,
		uint32(g.process.Pid), //nolint:gosec // Windows process IDs are DWORD values
	)
	if err != nil {
		return g.kill()
	}
	return nil
}

func (g *platformGroup) kill() error {
	if g.job != 0 {
		if err := windows.TerminateJobObject(g.job, jobTerminatedExitCode); err == nil {
			return nil
		}
	}
	return g.process.Kill()
}

func (g *platformGroup) close() {
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
}
