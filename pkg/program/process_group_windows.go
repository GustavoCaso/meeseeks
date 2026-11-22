//go:build windows

package program

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

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

// jobBasicAccounting mirrors JOBOBJECT_BASIC_ACCOUNTING_INFORMATION, which
// golang.org/x/sys/windows does not define.
type jobBasicAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

// setProcessGroup starts the command in a new console process group so it can
// receive CTRL_BREAK without affecting meeseeks. It starts suspended so no user
// code runs, and no child can escape, before the process joins its job.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED,
	}
}

// resume starts the suspended process. os/exec discards the primary thread
// handle, so the thread is found through a system snapshot.
func (g *platformGroup) resume() error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshot threads: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()

	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	pid := uint32(g.process.Pid) //nolint:gosec // Windows process IDs are DWORD values
	resumed := 0

	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}

		thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if openErr != nil {
			return fmt.Errorf("open thread: %w", openErr)
		}
		_, resumeErr := windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		if resumeErr != nil {
			return fmt.Errorf("resume thread: %w", resumeErr)
		}
		resumed++
	}

	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("enumerate threads: %w", err)
	}
	if resumed == 0 {
		return errors.New("no thread found for process")
	}
	return nil
}

// alive reports whether the job still has running processes.
func (g *platformGroup) alive() bool {
	if g.job == 0 {
		return false
	}

	var info jobBasicAccounting
	err := windows.QueryInformationJobObject(
		g.job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	)
	return err == nil && info.ActiveProcesses > 0
}

// attach assigns the suspended process to a new job.
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
