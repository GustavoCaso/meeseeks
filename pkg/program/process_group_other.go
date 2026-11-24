//go:build !windows && !linux && !darwin

package program

import "errors"

// waitLeaderExit reports that this platform cannot observe an exit without
// reaping. The group is then never signaled after Wait returns, which is safe
// but leaves descendants that outlive a terminated leader running.
func waitLeaderExit(int) bool {
	return false
}

func hasLiveDescendant(int) bool {
	return false
}

// processStartTime is unsupported here, so processes are not recorded and a
// reused PID can never be mistaken for a program left by a crashed daemon.
func processStartTime(int) (uint64, error) {
	return 0, errors.ErrUnsupported
}
