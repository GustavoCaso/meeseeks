//go:build linux

package program

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

// procStatStartTimeField is the position of starttime in /proc/<pid>/stat,
// counted from the state field that follows the command name.
const procStatStartTimeField = 22 - 3

// processStartTime reads the process start time in clock ticks since boot.
func processStartTime(pid int) (uint64, error) {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}

	// The command name can contain spaces and parentheses.
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, errors.New("malformed /proc stat")
	}
	fields := bytes.Fields(stat[end+1:])
	if len(fields) <= procStatStartTimeField {
		return 0, errors.New("malformed /proc stat")
	}
	return strconv.ParseUint(string(fields[procStatStartTimeField]), 10, 64)
}

// waitLeaderExit blocks until pid exits. WNOWAIT leaves it as a zombie, so its
// process group ID stays reserved.
func waitLeaderExit(pid int) bool {
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err == nil
	}
}

// hasLiveDescendant scans /proc for a non-zombie member of the group other than
// its leader. If /proc cannot be read it reports true, so the kill deadline
// still applies.
func hasLiveDescendant(pgid int) bool {
	return groupHasLiveMember(pgid, true)
}

// hasLiveMember is hasLiveDescendant counting the leader too.
func hasLiveMember(pgid int) bool {
	return groupHasLiveMember(pgid, false)
}

func groupHasLiveMember(pgid int, skipLeader bool) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}

	for _, entry := range entries {
		pid, convErr := strconv.Atoi(entry.Name())
		if convErr != nil || (skipLeader && pid == pgid) {
			continue
		}

		// A process can exit during the scan, so read errors are expected.
		stat, readErr := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if readErr != nil {
			continue
		}
		if statIsLiveMember(stat, pgid) {
			return true
		}
	}
	return false
}

// statIsLiveMember parses /proc/<pid>/stat. The command name can contain spaces
// and parentheses, so fields are read after its last closing parenthesis.
func statIsLiveMember(stat []byte, pgid int) bool {
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return false
	}

	var state string
	var ppid, group int
	if _, err := fmt.Sscan(string(stat[end+1:]), &state, &ppid, &group); err != nil {
		return false
	}
	return group == pgid && state != "Z" && state != "X"
}
