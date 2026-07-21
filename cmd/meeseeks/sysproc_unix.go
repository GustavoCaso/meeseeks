//go:build !windows

package main

import "syscall"

// detachSysProcAttr returns the attributes needed to fully detach the daemon
// from the controlling terminal by placing it in a new session.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true,
	}
}
