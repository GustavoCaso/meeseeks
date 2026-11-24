package program

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recordingTracker struct {
	mu      sync.Mutex
	started []ProcessIdentity
	exited  []ProcessIdentity
}

func (r *recordingTracker) ProcessStarted(id ProcessIdentity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, id)
}

func (r *recordingTracker) ProcessExited(id ProcessIdentity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exited = append(r.exited, id)
}

func (r *recordingTracker) snapshot() ([]ProcessIdentity, []ProcessIdentity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ProcessIdentity{}, r.started...), append([]ProcessIdentity{}, r.exited...)
}

func TestProgram_ReportsProcessIdentityWhileRunning(t *testing.T) {
	t.Parallel()

	tracker := &recordingTracker{}
	const grace = 3 * time.Second
	p := New("tracked", "sleep", Args("30"), Async(), KillGrace(grace), Tracker(tracker))

	done, err := p.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(time.Second) })

	started, exited := tracker.snapshot()
	if len(started) != 1 {
		t.Fatalf("reported %d started processes, want 1", len(started))
	}
	if len(exited) != 0 {
		t.Fatalf("reported %d exited processes while running, want 0", len(exited))
	}
	if started[0].PID <= 0 || started[0].StartTime == 0 {
		t.Fatalf("incomplete identity: %+v", started[0])
	}
	if started[0].KillGrace != grace {
		t.Fatalf("KillGrace = %s, want %s", started[0].KillGrace, grace)
	}

	if err = p.Shutdown(time.Second); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	<-done

	_, exited = tracker.snapshot()
	if len(exited) != 1 || exited[0] != started[0] {
		t.Fatalf("exited = %+v, want the started process %+v", exited, started[0])
	}
}
