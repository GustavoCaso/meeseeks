package program

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// ProcessIdentity identifies one OS process. A PID alone is not enough, because
// the OS reuses PIDs. The start time distinguishes a program's process from an
// unrelated process that later received the same PID.
type ProcessIdentity struct {
	PID       int           `json:"pid"`
	StartTime uint64        `json:"start_time"`
	KillGrace time.Duration `json:"kill_grace"`
}

// ProcessTracker is told when a program's process starts and ends, so a
// supervisor can find processes it left behind if it crashes.
type ProcessTracker interface {
	ProcessStarted(ProcessIdentity)
	ProcessExited(ProcessIdentity)
}

// Tracker reports this program's processes to tracker.
func Tracker(tracker ProcessTracker) Option {
	return func(p *program) {
		p.tracker = tracker
	}
}

// ErrInvalidOrphan means a recorded identity can never name a program's
// process, so retrying it is pointless.
var ErrInvalidOrphan = errors.New("invalid orphan record")

// TerminateOrphan stops the process group of a program left behind by a crashed
// supervisor, escalating to a forced kill after id.KillGrace.
//
// The identity comes from a file on disk, so it is verified first. A process
// that has exited, or whose PID now belongs to something else, is left alone.
// PIDs that would address other processes are refused, because signaling
// process group -1 reaches every process the user owns.
func TerminateOrphan(id ProcessIdentity) error {
	if id.PID <= 1 || id.PID == os.Getpid() {
		return fmt.Errorf("%w: refusing to terminate process %d", ErrInvalidOrphan, id.PID)
	}

	startTime, err := processStartTime(id.PID)
	if err != nil || startTime != id.StartTime {
		return nil //nolint:nilerr // A process that cannot be inspected has exited
	}
	return terminateOrphanGroup(id)
}
