package meeseeks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/program"
)

type Meeseek interface {
	AddProgram(prog program.Program, interval ...time.Duration) error
	Start(ctx context.Context)
	Stop(programName string, timeout time.Duration) error
	Wait(ctx context.Context) error
	Statistic(program string) (program.Statistics, error)
	Statistics() []program.Statistics
	Shutdown(timeout time.Duration) error
}

// ProgramInfo holds program metadata for unified storage.
type ProgramInfo struct {
	Program  program.Program
	Interval *time.Duration // nil for regular programs, non-nil for scheduled
}

type meeseek struct {
	startTime      time.Time
	endTime        time.Time
	programs       map[string]*ProgramInfo  // Unified storage for all programs
	schedulerStops map[string]chan struct{} // Only for scheduled programs
	wg             *sync.WaitGroup
	mu             sync.RWMutex
}

func (m *meeseek) AddProgram(prog program.Program, interval ...time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := prog.Name()
	if _, exists := m.programs[name]; exists {
		return fmt.Errorf("duplicated %s program", name)
	}

	// Create program info with optional interval
	progInfo := &ProgramInfo{
		Program: prog,
	}
	if len(interval) > 0 && interval[0] > 0 {
		progInfo.Interval = &interval[0]
	}

	m.programs[name] = progInfo
	return nil
}

func (m *meeseek) Start(ctx context.Context) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.startTime = time.Now()

	// Count regular programs for WaitGroup
	regularCount := 0
	for _, info := range m.programs {
		if info.Interval == nil {
			regularCount++
		}
	}
	m.wg.Add(regularCount)

	// Start all programs
	for _, info := range m.programs {
		if info.Interval == nil {
			// Start regular program
			go func(prog program.Program) {
				done, err := prog.Start(ctx)
				if err != nil {
					slog.Error("failed to start program", "program", prog.Name(), "error", err.Error())
				}
				<-done
				m.wg.Done()
			}(info.Program)
		} else {
			// Start scheduled program
			go func(progInfo *ProgramInfo) {
				m.runScheduledProgram(ctx, progInfo.Program, *progInfo.Interval)
			}(info)
		}
	}
}

func (m *meeseek) runScheduledProgram(ctx context.Context, prog program.Program, interval time.Duration) {
	programName := prog.Name()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Create a stop channel for this scheduled program
	stop := make(chan struct{})
	m.mu.Lock()
	m.schedulerStops[programName] = stop
	m.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			done, err := prog.Start(ctx)
			if err != nil {
				return
			}

			select {
			case <-done:
				// Program execution completed normally
			case <-ctx.Done():
				// Context cancelled while program was running
				return
			case <-stop:
				// Stop signal while program was running - shutdown the current execution
				_ = prog.Shutdown(5 * time.Second)
				return
			}
		}
	}
}

func (m *meeseek) Statistic(programName string) (program.Statistics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if info, ok := m.programs[programName]; ok {
		return info.Program.Statistics(), nil
	}

	return program.Statistics{}, fmt.Errorf("program %s not present", programName)
}

func (m *meeseek) Statistics() []program.Statistics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statistics := make([]program.Statistics, 0, len(m.programs))

	for _, info := range m.programs {
		statistics = append(statistics, info.Program.Statistics())
	}

	return statistics
}

func (m *meeseek) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	defer func() {
		m.endTime = time.Now()
	}()

	select {
	case <-ctx.Done():
		return errors.New("context cancelled while waiting for programs to finalize")
	case <-done:
		return nil
	}
}

func (m *meeseek) Stop(programName string, timeout time.Duration) error {
	m.mu.RLock()
	info, ok := m.programs[programName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("program %s not present", programName)
	}

	if info.Interval == nil {
		// Regular program - shutdown directly
		return info.Program.Shutdown(timeout)
	}

	// Scheduled program - stop via channel
	m.mu.Lock()
	if stop, exists := m.schedulerStops[programName]; exists {
		close(stop)
		delete(m.schedulerStops, programName)
	}
	m.mu.Unlock()
	return nil
}

func (m *meeseek) Shutdown(timeout time.Duration) error {
	// Stop all scheduled programs
	m.mu.Lock()
	for _, stop := range m.schedulerStops {
		close(stop)
	}
	m.schedulerStops = make(map[string]chan struct{})
	m.mu.Unlock()

	errs := make([]error, 0, len(m.programs))

	for _, info := range m.programs {
		errs = append(errs, info.Program.Shutdown(timeout))
	}

	return errors.Join(errs...)
}

func New() Meeseek {
	return &meeseek{
		wg:             &sync.WaitGroup{},
		programs:       make(map[string]*ProgramInfo),
		schedulerStops: make(map[string]chan struct{}),
	}
}
