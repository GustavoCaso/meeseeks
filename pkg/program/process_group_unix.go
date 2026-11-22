//go:build !windows

package program

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// platformGroup addresses a POSIX process group led by process.
type platformGroup struct {
	process *os.Process
}

// setProcessGroup makes the command the leader of a new process group.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func (g *platformGroup) attach(process *os.Process) error {
	g.process = process
	return nil
}

// resume is a no-op because a Unix process joins its group before it executes.
func (g *platformGroup) resume() error {
	return nil
}

// alive reports whether the group still has a member that can be signaled.
// EPERM counts as empty because macOS returns it for a group of only zombies.
func (g *platformGroup) alive() bool {
	return syscall.Kill(-g.process.Pid, 0) == nil
}

func (g *platformGroup) terminate() error {
	return g.signal(syscall.SIGTERM)
}

func (g *platformGroup) kill() error {
	return g.signal(syscall.SIGKILL)
}

func (g *platformGroup) close() {}

func (g *platformGroup) signal(signal syscall.Signal) error {
	err := syscall.Kill(-g.process.Pid, signal)

	switch {
	case errors.Is(err, syscall.ESRCH):
		return os.ErrProcessDone
	case errors.Is(err, syscall.EPERM):
		// macOS reports EPERM for a group containing only zombies.
		return g.process.Signal(signal)
	default:
		return err
	}
}
