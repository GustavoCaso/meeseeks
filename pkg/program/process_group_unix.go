//go:build !windows

package program

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
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

// waitLeader blocks until the leader exits and leaves it unreaped. It returns
// false if the platform cannot observe that.
func (g *platformGroup) waitLeader() bool {
	return waitLeaderExit(g.process.Pid)
}

// alive reports whether a process other than the exited leader is still live
// in the group. kill(pgid, 0) cannot answer this, because the zombie leader
// still counts as a member.
func (g *platformGroup) alive() bool {
	return hasLiveDescendant(g.process.Pid)
}

// isClosedPipeError reports whether a stdin write failed because the program
// closed its end of the pipe.
func isClosedPipeError(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}

func (g *platformGroup) terminate() error {
	return g.signal(syscall.SIGTERM)
}

func (g *platformGroup) kill() error {
	return g.signal(syscall.SIGKILL)
}

func (g *platformGroup) close() {}

// terminateOrphanGroup asks the group to exit, then force-kills what remains
// after its grace period. Only a verified group leader is signaled, because the
// group ID equals the leader's PID.
func terminateOrphanGroup(id ProcessIdentity) error {
	pgid, err := syscall.Getpgid(id.PID)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	if pgid != id.PID {
		return nil
	}

	if err = syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		if orphanGroupGone(err) {
			return nil
		}
		return err
	}
	if waitForOrphanGroup(pgid, id.KillGrace) {
		return nil
	}

	if err = syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !orphanGroupGone(err) {
		return err
	}
	if !waitForOrphanGroup(pgid, killSettleTime) {
		return fmt.Errorf("process group %d still running after SIGKILL", pgid)
	}
	return nil
}

// waitForOrphanGroup reports whether the group stopped within limit. A group of
// only zombies has stopped, even though Linux still lets it be signaled until
// its new parent reaps it, which may be late or never.
func waitForOrphanGroup(pgid int, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		if orphanGroupGone(syscall.Kill(-pgid, 0)) || !hasLiveMember(pgid) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(groupPollInterval)
	}
}

// orphanGroupGone interprets a signal result. macOS reports EPERM for a group
// containing only zombies.
func orphanGroupGone(err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM)
}

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
