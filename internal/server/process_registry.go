package server

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sync"

	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

// processRegistry records running program processes on disk. After a crash the
// file lists what the old daemon left behind, so the next daemon can stop those
// processes before launching new copies.
type processRegistry struct {
	logger    *logger.Logger
	path      string
	processes []program.ProcessIdentity
	mu        sync.Mutex
}

func newProcessRegistry(path string, logger *logger.Logger) *processRegistry {
	return &processRegistry{path: path, logger: logger}
}

// ProcessStarted implements program.ProcessTracker.
func (r *processRegistry) ProcessStarted(id program.ProcessIdentity) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.processes = append(r.processes, id)
	r.saveLocked()
}

// ProcessExited implements program.ProcessTracker.
func (r *processRegistry) ProcessExited(id program.ProcessIdentity) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.processes = slices.DeleteFunc(r.processes, func(p program.ProcessIdentity) bool {
		return p == id
	})
	r.saveLocked()
}

// stopOrphans stops every process recorded by a previous daemon. They stop
// concurrently, so one slow program does not delay the others. The caller must
// hold the instance lock, which proves the recording daemon is gone.
func (r *processRegistry) stopOrphans() {
	r.mu.Lock()
	defer r.mu.Unlock()

	orphans := r.loadLocked()
	if len(orphans) == 0 {
		return
	}

	var stopping sync.WaitGroup
	for _, orphan := range orphans {
		stopping.Go(func() {
			r.logger.Warn("stopping program left by a previous daemon", "pid", orphan.PID)
			if err := program.TerminateOrphan(orphan); err != nil {
				r.logger.Error(
					"could not stop program left by a previous daemon",
					"pid", orphan.PID,
					"error", err.Error(),
				)
			}
		})
	}
	stopping.Wait()

	r.processes = nil
	r.saveLocked()
}

// loadLocked treats an unreadable file as empty, because a bad record must not
// prevent the daemon from starting.
func (r *processRegistry) loadLocked() []program.ProcessIdentity {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		r.logger.Warn("could not read process registry", "error", err.Error())
		return nil
	}

	var processes []program.ProcessIdentity
	if err = json.Unmarshal(data, &processes); err != nil {
		r.logger.Warn("ignoring corrupt process registry", "error", err.Error())
		return nil
	}
	return processes
}

// saveLocked replaces the file atomically, so a crash mid-write cannot leave a
// partial record.
func (r *processRegistry) saveLocked() {
	if len(r.processes) == 0 {
		if err := os.Remove(r.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			r.logger.Warn("could not clear process registry", "error", err.Error())
		}
		return
	}

	data, err := json.Marshal(r.processes)
	if err != nil {
		r.logger.Warn("could not encode process registry", "error", err.Error())
		return
	}

	tmp := r.path + ".tmp"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		r.logger.Warn("could not write process registry", "error", err.Error())
		return
	}
	if err = os.Rename(tmp, r.path); err != nil {
		r.logger.Warn("could not replace process registry", "error", err.Error())
	}
}
