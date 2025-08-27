package meeseeks

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/logger"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

const timeDriftCheckDuration = 30 * time.Second

type Meeseek interface {
	AddProgram(prog program.Program, interval ...time.Duration) error
	Start(ctx context.Context)
	Stop(programName string, timeout time.Duration) error
	Wait(ctx context.Context) error
	Statistic(program string) (Statistics, error)
	Statistics() []Statistics
	Shutdown(timeout time.Duration) error
}

// ProgramInfo holds program metadata for unified storage.
type ProgramInfo struct {
	Program  program.Program
	Interval *time.Duration // nil for regular programs, non-nil for scheduled
}

// Statistics tracks individual program execution statistics.
type Statistics struct {
	ProgramName      string `json:"program_name"`
	State            string `json:"state"`
	Successful       int    `json:"successful_runs"`
	Failed           int    `json:"failed_runs"`
	TotalOutputLines int    `json:"total_output_lines"`
	LastError        string `json:"last_error"`
	LastOutput       string `json:"last_output"`
	Interval         string `json:"interval,omitempty"`
	LastRunAt        string `json:"last_run_at"`
	NextRunAt        string `json:"next_run,omitempty"`
}

type executionTrack struct {
	successful int
	failed     int
	lastRunAt  time.Time
}

type meeseek struct {
	startTime      time.Time
	endTime        time.Time
	programs       map[string]*ProgramInfo    // Unified storage for all programs
	schedulerStops map[string]chan struct{}   // Only for scheduled programs
	executions     map[string]*executionTrack // execution tracking for each program
	wg             *sync.WaitGroup
	mu             sync.RWMutex
	logger         logger.Logger
}

func (m *meeseek) AddProgram(prog program.Program, interval ...time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := prog.Name()
	if _, exists := m.programs[name]; exists {
		return fmt.Errorf("duplicated %s program", name)
	}

	progInfo := &ProgramInfo{
		Program: prog,
	}
	if len(interval) > 0 && interval[0] > 0 {
		progInfo.Interval = &interval[0]
	}

	m.programs[name] = progInfo
	m.executions[name] = &executionTrack{}
	return nil
}

func (m *meeseek) Start(ctx context.Context) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.startTime = time.Now()

	m.wg.Add(len(m.programs))

	// Start all programs
	for _, info := range m.programs {
		if info.Interval == nil {
			// Start regular program
			go func(prog program.Program) {
				m.runOneTimeProgram(ctx, prog)
			}(info.Program)
		} else {
			// Start scheduled program
			go func(progInfo *ProgramInfo) {
				m.runScheduledProgram(ctx, progInfo.Program, *progInfo.Interval)
			}(info)
		}
	}
}

func (m *meeseek) runOneTimeProgram(ctx context.Context, prog program.Program) {
	defer m.wg.Done()

	done, err := prog.Start(ctx)
	if err != nil {
		if m.logger != nil {
			m.logger.Error("program failed", "program", prog.Name(), "error", err.Error())
		}
		m.trackProgramCompletion(prog.Name(), false)
		return
	}
	<-done
	if m.logger != nil {
		m.logger.Info(
			"program executed",
			"program",
			prog.Name(),
			"state",
			program.StateToString[prog.State()],
		)
	}
	// Track completion success based on final state
	m.trackProgramCompletion(prog.Name(), prog.State() == program.StateFinished)
}

func (m *meeseek) runScheduledProgram(ctx context.Context, prog program.Program, interval time.Duration) {
	programName := prog.Name()
	ticker := time.NewTicker(interval)
	timeDriftTicker := time.NewTicker(timeDriftCheckDuration)
	defer timeDriftTicker.Stop()
	defer ticker.Stop()
	defer m.wg.Done()

	// Create and register stop channel early to avoid race conditions
	stop := make(chan struct{})
	m.mu.Lock()
	m.schedulerStops[programName] = stop
	m.mu.Unlock()

	// Cleanup stop channel on exit
	defer func() {
		m.mu.Lock()
		delete(m.schedulerStops, programName)
		m.mu.Unlock()
	}()

	// Execute the program immediately first
	m.executeScheduledProgram(ctx, prog, "initial")

	// Main interval loop
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			m.executeScheduledProgram(ctx, prog, "interval")
		case <-timeDriftTicker.C:
			m.mu.RLock()
			exec := m.executions[prog.Name()]
			m.mu.RUnlock()

			if time.Since(exec.lastRunAt) > interval {
				m.executeScheduledProgram(ctx, prog, "drift_recovery")
			}
		}
	}
}

// executeScheduledProgram handles execution of a scheduled program with consistent error handling.
func (m *meeseek) executeScheduledProgram(ctx context.Context, prog program.Program, executionType string) {
	programName := prog.Name()

	// Check if we should stop before starting execution
	m.mu.RLock()
	stop, exists := m.schedulerStops[programName]
	m.mu.RUnlock()

	if !exists {
		// Stop channel was deleted, scheduler is shutting down
		return
	}

	done, err := prog.Start(ctx)
	if err != nil {
		if m.logger != nil {
			m.logger.Error("scheduled program failed to start",
				"program", prog.Name(),
				"execution_type", executionType,
				"error", err.Error())
		}
		m.trackProgramCompletion(programName, false)
		// Continue running for interval programs even if one execution fails
		return
	}

	select {
	case <-done:
		// Program execution completed normally
		success := prog.State() == program.StateFinished
		if m.logger != nil {
			m.logger.Info(
				"scheduled program executed",
				"program", prog.Name(),
				"execution_type", executionType,
				"state", program.StateToString[prog.State()],
			)
		}
		m.trackProgramCompletion(programName, success)
	case <-ctx.Done():
		// Context cancelled while program was running
		return
	case <-stop:
		// Stop signal while program was running - shutdown gracefully
		_ = prog.Shutdown(5 * time.Second)
		return
	}
}

func (m *meeseek) Statistic(programName string) (Statistics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.programs[programName]
	if !ok {
		return Statistics{}, fmt.Errorf("program %s not present", programName)
	}
	execRun, ok := m.executions[programName]
	if !ok {
		return Statistics{}, fmt.Errorf("execution information for %s not present", programName)
	}

	return m.collectProgramStatistics(info, execRun), nil
}

func (m *meeseek) Statistics() []Statistics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statistics := make([]Statistics, len(m.programs))
	keys := slices.Sorted(maps.Keys(m.programs))

	for i, key := range keys {
		info := m.programs[key]
		execRecord := m.executions[key]
		statistics[i] = m.collectProgramStatistics(info, execRecord)
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
	stop, exists := m.schedulerStops[programName]
	m.mu.Unlock()

	if exists {
		select {
		case <-stop:
			// Channel already closed
		default:
			close(stop)
		}
		// Note: cleanup is handled by the defer in runScheduledProgram
	}

	return nil
}

func (m *meeseek) Shutdown(timeout time.Duration) error {
	// Stop all scheduled programs
	m.mu.Lock()
	for _, stop := range m.schedulerStops {
		select {
		case <-stop:
			// Channel already closed
		default:
			close(stop)
		}
	}
	m.schedulerStops = make(map[string]chan struct{})
	m.mu.Unlock()

	errs := make([]error, 0, len(m.programs))

	for _, info := range m.programs {
		errs = append(errs, info.Program.Shutdown(timeout))
	}

	return errors.Join(errs...)
}

// collectProgramStatistics gathers statistics from program state and execution records.
func (m *meeseek) collectProgramStatistics(info *ProgramInfo, execRecord *executionTrack) Statistics {
	prog := info.Program
	programName := prog.Name()
	progState := prog.State()

	stats := Statistics{
		ProgramName: programName,
		State:       program.StateToString[progState],
		Successful:  execRecord.successful,
		Failed:      execRecord.failed,
		LastRunAt:   execRecord.lastRunAt.Format(time.DateTime),
	}

	if info.Interval != nil {
		stats.Interval = info.Interval.String()
		stats.NextRunAt = execRecord.lastRunAt.Add(*info.Interval).Format(time.DateTime)
	}

	// Collect last output and error information
	lastOutput := prog.LastLine()
	lastError := ""
	if progState == program.StateError {
		lastError = strings.TrimSpace(prog.Error())
		if len(lastError) > 200 { // Limit error message length for readability
			lastError = lastError[:200] + "..."
		}
	}

	// Update last output information
	if lastOutput != "" {
		stats.LastOutput = lastOutput
	}
	if lastError != "" {
		stats.LastError = lastError
	}

	// Count output lines
	output := prog.Output()
	if output != "" {
		stats.TotalOutputLines = strings.Count(output, "\n")
	}

	return stats
}

// trackProgramCompletion updates execution statistics when a program completes.
func (m *meeseek) trackProgramCompletion(programName string, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	execRecord := m.executions[programName]

	if success {
		execRecord.successful++
	} else {
		execRecord.failed++
	}
	execRecord.lastRunAt = time.Now()
}

type Option func(*meeseek)

func Logger(logger logger.Logger) Option {
	return func(m *meeseek) {
		m.logger = logger
	}
}

func New(opts ...Option) Meeseek {
	m := &meeseek{
		wg:             &sync.WaitGroup{},
		programs:       make(map[string]*ProgramInfo),
		schedulerStops: make(map[string]chan struct{}),
		executions:     make(map[string]*executionTrack),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}
