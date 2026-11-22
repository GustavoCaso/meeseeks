//go:build !windows && !linux && !darwin

package program

// waitLeaderExit reports that this platform cannot observe an exit without
// reaping. The group is then never signaled after Wait returns, which is safe
// but leaves descendants that outlive a terminated leader running.
func waitLeaderExit(int) bool {
	return false
}

func hasLiveDescendant(int) bool {
	return false
}
