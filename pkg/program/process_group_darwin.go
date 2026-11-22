//go:build darwin

package program

import (
	"errors"

	"golang.org/x/sys/unix"
)

// darwinZombieState is the kernel's SZOMB process state.
const darwinZombieState = 5

// waitLeaderExit blocks until pid exits, using kqueue because it does not reap
// the process and so keeps its group ID reserved.
func waitLeaderExit(pid int) bool {
	kq, err := unix.Kqueue()
	if err != nil {
		return false
	}
	defer func() { _ = unix.Close(kq) }()

	change := []unix.Kevent_t{{
		Ident:  uint64(pid), //nolint:gosec // Process IDs are positive
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}}
	events := make([]unix.Kevent_t, 1)

	for {
		_, err = unix.Kevent(kq, change, events, nil)
		switch {
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.ESRCH):
			// The process exited before the event was registered.
			return true
		default:
			return err == nil
		}
	}
}

// hasLiveDescendant asks the kernel for the group's members. If the query fails
// it reports true, so the kill deadline still applies.
func hasLiveDescendant(pgid int) bool {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return true
	}

	for i := range procs {
		if int(procs[i].Proc.P_pid) != pgid && procs[i].Proc.P_stat != darwinZombieState {
			return true
		}
	}
	return false
}
