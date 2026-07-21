//go:build windows

package main

import "syscall"

// Windows process creation flags for detaching the daemon from the parent
// console. See https://learn.microsoft.com/windows/win32/procthread/process-creation-flags
const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detachSysProcAttr returns the attributes needed to fully detach the daemon
// from the parent console: it runs in its own process group with no console
// window attached.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | detachedProcess,
		HideWindow:    true,
	}
}
