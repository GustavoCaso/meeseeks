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

// waitLeader blocks until the leader exits. os.Process keeps its handle open
// until Wait, so the process ID cannot be reused during this call.
func (g *platformGroup) waitLeader() bool {
	handle, err := windows.OpenProcess(
		windows.SYNCHRONIZE,
		false,
		uint32(g.process.Pid), //nolint:gosec // Windows process IDs are DWORD values
	)
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	event, err := windows.WaitForSingleObject(handle, windows.INFINITE)
	return err == nil && event == windows.WAIT_OBJECT_0
}

// isClosedPipeError reports whether a stdin write failed because the program
// closed its end of the pipe. Windows has two errors for this.
func isClosedPipeError(err error) bool {
	return errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_NO_DATA)
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

// processStartTime reads the process creation time as a FILETIME value.
func processStartTime(pid int) (uint64, error) {
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(pid), //nolint:gosec // Windows process IDs are DWORD values
	)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	var creation, exit, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	return uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime), nil
}

// jobName derives a job's name from its leader's identity, so a restarted
// daemon can reopen the job of a process left behind by a crash.
func jobName(pid int, startTime uint64) (*uint16, error) {
	return windows.UTF16PtrFromString(fmt.Sprintf(`Local\meeseeks-%d-%d`, pid, startTime))
}

// terminateOrphanGroup kills the process tree through its named job. Windows
// has no graceful signal without a shared console, so KillGrace does not apply.
// CreateJobObject opens the job if it exists. If attaching failed originally
// the new job is empty, so the leader is also killed directly.
func terminateOrphanGroup(id ProcessIdentity) error {
	if name, err := jobName(id.PID, id.StartTime); err == nil {
		if job, jobErr := windows.CreateJobObject(nil, name); jobErr == nil {
			defer func() { _ = windows.CloseHandle(job) }()
			_ = windows.TerminateJobObject(job, jobTerminatedExitCode)
		}
	}

	// A failed lookup means the job already ended the process.
	process, err := os.FindProcess(id.PID)
	if err != nil {
		return nil
	}
	defer func() { _ = process.Release() }()

	err = process.Kill()
	// A process the job just terminated can report access denied while exiting.
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return nil
	}
	return err
}

// attach assigns the suspended process to a new job. A job that cannot be
// named still works, but cannot be found again after a daemon crash.
func (g *platformGroup) attach(process *os.Process) error {
	g.process = process

	var name *uint16
	if startTime, timeErr := processStartTime(process.Pid); timeErr == nil {
		name, _ = jobName(process.Pid, startTime)
	}

	job, err := windows.CreateJobObject(nil, name)
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
