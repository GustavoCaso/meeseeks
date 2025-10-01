// Package meeseeks provides a process manager that orchestrates multiple programs.
// It supports both one-time execution and interval-based scheduling with comprehensive
// monitoring, statistics, and management capabilities.
package meeseeks

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/logger"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

const intervalCheckDuration = 1 * time.Second

// Program extends the program.Program interface with interval scheduling capabilities.
// It represents a program that can be managed by Meeseeks with optional interval-based execution.
type Program interface {
	program.Program
	Interval() *time.Duration
	Equal(Program) bool
}

type meeseekProgram struct {
	program.Program
	interval *time.Duration
}

// Interval returns the execution interval for this program.
// Returns nil for one-time programs or a duration pointer for scheduled programs.
func (m *meeseekProgram) Interval() *time.Duration {
	return m.interval
}

// Equal performs semantic comparison of two programs to determine if they have
// identical configuration. This compares:
// - Program string representation (name, command, arguments)
// - Interval configuration (value, not pointer)
// This avoids the fragility of reflect.DeepEqual with pointers and runtime state.
func (m *meeseekProgram) Equal(other Program) bool {
	// Compare program string representation (includes name, command, args)
	if m.Program.String() != other.String() {
		return false
	}

	// Compare interval configuration
	otherInterval := other.Interval()
	if m.interval == nil && otherInterval == nil {
		return true
	}
	if m.interval == nil || otherInterval == nil {
		return false
	}
	// Compare interval values, not pointers
	return *m.interval == *otherInterval
}

// NewProgram creates a new Program instance that wraps a program.Program with optional interval scheduling.
// If interval is nil, the program will execute once. If interval is provided, the program will execute
// repeatedly at the specified interval.
func NewProgram(prog program.Program, interval *time.Duration) Program {
	return &meeseekProgram{
		Program:  prog,
		interval: interval,
	}
}

// Meeseek defines the interface for managing multiple programs with scheduling capabilities.
// It provides methods for adding programs, controlling execution, monitoring statistics,
// and managing the lifecycle of all managed programs.
type Meeseek interface {
	// AddProgram adds a new program to the meeseeks manager.
	// Returns an error if a program with the same name already exists.
	AddProgram(Program) error
	// Programs returns a sorted list of all managed program names with their command details.
	Programs() []string
	// Reload performs a hot reload of the meeseeks configuration with new programs.
	//
	// This method prioritizes data integrity over operational availability by holding an exclusive
	// lock for the entire reload duration. This means ALL other operations (Statistics, Status,
	// AddProgram, etc.) are blocked until reload completes.
	//
	// If an error happens while shutdown of previous programs we log the error but continue the reload process.
	Reload(context.Context, []Program, time.Duration)
	// Start begins execution of all managed programs according to their configuration.
	// One-time programs start immediately, while interval programs start their scheduling.
	Start(ctx context.Context)
	// Stop gracefully stops a specific program with the given timeout.
	// Returns an error if the program is not found or cannot be stopped gracefully.
	Stop(programName string, timeout time.Duration) error
	// Run executes a specific program immediately, regardless of its scheduling configuration.
	// Returns an error if the program is not found or is already running.
	Run(programName string) error
	// Wait blocks until all programs have finished execution or the context is cancelled.
	// Returns an error if the context is cancelled before completion.
	Wait(ctx context.Context) error
	// Statistic returns detailed execution statistics for a specific program.
	// Returns an error if the program is not found.
	Statistic(program string) (Statistics, error)
	// Statistics returns execution statistics for all managed programs.
	// The map is keyed by program name.
	Statistics() map[string]Statistics
	// Shutdown gracefully stops all managed programs with the specified timeout.
	// Returns any errors encountered during the shutdown process.
	Shutdown(timeout time.Duration) error
}

// Statistics tracks individual program execution statistics.
type Statistics struct {
	ProgramName string `json:"program_name"`
	State       string `json:"state"`
	Output      string `json:"output"`
	Error       string `json:"error"`
	Interval    string `json:"interval,omitempty"`
	LastRunAt   string `json:"last_run_at"`
	NextRunAt   string `json:"next_run,omitempty"`
	Successful  int    `json:"successful_runs"`
	Failed      int    `json:"failed_runs"`
}

type executionTrack struct {
	lastRunAt  time.Time
	successful int
	failed     int
}

type meeseek struct {
	startTime      time.Time
	endTime        time.Time
	logger         logger.Logger
	programs       map[string]Program
	schedulerStops map[string]chan struct{}
	executions     map[string]*executionTrack
	wg             *sync.WaitGroup
	mu             sync.RWMutex
}

func (m *meeseek) AddProgram(prog Program) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := prog.Name()
	if _, exists := m.programs[name]; exists {
		return fmt.Errorf("duplicated %s program", name)
	}

	m.programs[name] = prog
	m.executions[name] = &executionTrack{}
	return nil
}

func (m *meeseek) Start(ctx context.Context) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.startTime = time.Now()

	// Start all programs
	for _, program := range m.programs {
		m.wg.Add(1)
		go func(prog Program) {
			m.runProgram(ctx, prog)
			m.wg.Done()
		}(program)
	}
}

func (m *meeseek) Programs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := slices.Sorted(maps.Keys(m.programs))

	programs := make([]string, len(m.programs))

	for i, key := range keys {
		programs[i] = m.programs[key].String()
	}

	return programs
}

func (m *meeseek) Reload(ctx context.Context, programs []Program, deadline time.Duration) {
	if len(programs) == 0 {
		return
	}

	newPrograms := map[string]Program{}
	for _, prog := range programs {
		newPrograms[prog.Name()] = prog
	}

	m.mu.Lock()

	removeState := []string{}
	keepState := []string{}

	for name, oldProgram := range m.programs {
		newProgram, exists := newPrograms[name]
		if !exists {
			removeState = append(removeState, name)
		} else {
			if oldProgram.Equal(newProgram) {
				keepState = append(keepState, name)
			} else {
				removeState = append(removeState, name)
			}
		}
	}

	m.wg.Add(1)
	defer m.wg.Done()

	m.resetSchedulerStops()

	err := m.shutdown(deadline)
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("error shutding down old programs while reloading", "error", err.Error())
		}
	}

	for _, name := range removeState {
		delete(m.executions, name)
	}

	m.programs = map[string]Program{}

	for name, prog := range newPrograms {
		m.programs[name] = prog
		if !slices.Contains(keepState, name) {
			m.executions[name] = &executionTrack{}
		}
	}

	m.mu.Unlock()

	m.Start(ctx)
}

func (m *meeseek) Statistic(programName string) (Statistics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	program, ok := m.programs[programName]
	if !ok {
		return Statistics{}, fmt.Errorf("program %s not present", programName)
	}
	execRun, ok := m.executions[programName]
	if !ok {
		return Statistics{}, fmt.Errorf("execution information for %s not present", programName)
	}

	return m.collectProgramStatistics(program, execRun), nil
}

func (m *meeseek) Statistics() map[string]Statistics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statistics := map[string]Statistics{}
	keys := slices.Sorted(maps.Keys(m.programs))

	for _, key := range keys {
		program := m.programs[key]
		execRecord := m.executions[key]
		statistics[key] = m.collectProgramStatistics(program, execRecord)
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

func (m *meeseek) Run(programName string) error {
	m.mu.RLock()
	prog, ok := m.programs[programName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("program %s not present", programName)
	}

	state := prog.State()
	if state == program.StateRunning {
		return fmt.Errorf("program %s already running", programName)
	}

	m.runOneTimeProgram(context.Background(), prog)

	return nil
}

func (m *meeseek) Stop(programName string, timeout time.Duration) error {
	m.mu.RLock()
	program, ok := m.programs[programName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("program %s not present", programName)
	}

	if program.Interval() == nil {
		// Regular program - shutdown directly
		return program.Shutdown(timeout)
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
	m.resetSchedulerStops()
	m.mu.Unlock()

	return m.shutdown(timeout)
}

// this function is meant to be called while holding the lock.
func (m *meeseek) resetSchedulerStops() {
	for _, stop := range m.schedulerStops {
		select {
		case <-stop:
			// Channel already closed
		default:
			close(stop)
		}
	}
	m.schedulerStops = make(map[string]chan struct{})
}

func (m *meeseek) shutdown(timeout time.Duration) error {
	errs := make([]error, 0, len(m.programs))

	for _, program := range m.programs {
		errs = append(errs, program.Shutdown(timeout))
	}

	return errors.Join(errs...)
}

func (m *meeseek) runProgram(ctx context.Context, prog Program) {
	interval := prog.Interval()
	if interval != nil && *interval > 0 {
		m.runScheduledProgram(ctx, prog)
	} else {
		m.runOneTimeProgram(ctx, prog)
	}
}

func (m *meeseek) runOneTimeProgram(ctx context.Context, prog Program) {
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

func (m *meeseek) runScheduledProgram(ctx context.Context, prog Program) {
	programName := prog.Name()
	interval := *prog.Interval()
	ticker := time.NewTicker(intervalCheckDuration)

	defer ticker.Stop()

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
			m.mu.RLock()
			exec := m.executions[prog.Name()]
			m.mu.RUnlock()

			// We strip the monotonomic clock information using Round(0) because in some system
			// will stop if the computer goes to sleep. That way we ensure calling now.Sub() is
			// accurate
			now := time.Now().Round(0)
			strippedLastRunAt := exec.lastRunAt.Round(0)
			timeSinceLastRun := now.Sub(strippedLastRunAt)
			execute := timeSinceLastRun >= interval

			if execute {
				m.executeScheduledProgram(ctx, prog, "interval")
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

func (m *meeseek) collectProgramStatistics(prog Program, execRecord *executionTrack) Statistics {
	programName := prog.Name()
	progState := prog.State()

	stats := Statistics{
		ProgramName: programName,
		State:       program.StateToString[progState],
		Successful:  execRecord.successful,
		Failed:      execRecord.failed,
		LastRunAt:   execRecord.lastRunAt.Format(time.DateTime),
	}

	interval := prog.Interval()

	if interval != nil && *interval > 0 {
		stats.Interval = interval.String()
		stats.NextRunAt = execRecord.lastRunAt.Add(*interval).Format(time.DateTime)
	}

	stats.Error = prog.Error()
	stats.Output = prog.Output()

	return stats
}

// trackProgramCompletion updates execution statistics when a program completes.
func (m *meeseek) trackProgramCompletion(programName string, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	execRecord := m.executions[programName]
	if execRecord == nil {
		// Program was removed during reload, ignore completion tracking
		return
	}

	if success {
		execRecord.successful++
	} else {
		execRecord.failed++
	}
	execRecord.lastRunAt = time.Now()
}

// Option defines a function type for configuring meeseeks instances.
type Option func(*meeseek)

// Logger sets the logger instance for the meeseeks manager.
// The logger will be used for all internal logging operations.
func Logger(logger logger.Logger) Option {
	return func(m *meeseek) {
		m.logger = logger
	}
}

// New creates a new Meeseek instance with the provided options.
// The returned instance is ready to have programs added and can be started.
func New(opts ...Option) Meeseek {
	m := &meeseek{
		wg:             &sync.WaitGroup{},
		programs:       make(map[string]Program),
		schedulerStops: make(map[string]chan struct{}),
		executions:     make(map[string]*executionTrack),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}
