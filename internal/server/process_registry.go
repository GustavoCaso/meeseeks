package server

import (
	"encoding/json"
	"errors"
	"fmt"
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

	// terminate stops one orphan. Tests replace it to simulate failures.
	terminate func(program.ProcessIdentity) error
}

func newProcessRegistry(path string, logger *logger.Logger) *processRegistry {
	return &processRegistry{path: path, logger: logger, terminate: program.TerminateOrphan}
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
//
// A process that could not be stopped stays recorded and an error is returned,
// so the daemon does not launch a second copy next to it and a later start can
// retry the cleanup.
func (r *processRegistry) stopOrphans() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	orphans := r.loadLocked()
	if len(orphans) == 0 {
		return nil
	}

	var (
		stopping sync.WaitGroup
		failedMu sync.Mutex
		failed   []program.ProcessIdentity
		errs     []error
	)
	for _, orphan := range orphans {
		stopping.Go(func() {
			r.logger.Warn("stopping program left by a previous daemon", "pid", orphan.PID)
			err := r.terminate(orphan)
			if errors.Is(err, program.ErrInvalidOrphan) {
				// Never a program's process, so keeping it would only block
				// every later start.
				r.logger.Warn("dropping invalid process record", "pid", orphan.PID)
				return
			}
			if err != nil {
				r.logger.Error(
					"could not stop program left by a previous daemon",
					"pid", orphan.PID,
					"error", err.Error(),
				)
				failedMu.Lock()
				failed = append(failed, orphan)
				errs = append(errs, fmt.Errorf("process %d: %w", orphan.PID, err))
				failedMu.Unlock()
			}
		})
	}
	stopping.Wait()

	r.processes = failed
	r.saveLocked()

	if len(errs) > 0 {
		return fmt.Errorf(
			"could not stop programs left by a previous daemon, stop them manually or remove %s: %w",
			r.path,
			errors.Join(errs...),
		)
	}
	return nil
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
