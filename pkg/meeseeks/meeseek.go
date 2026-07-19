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

// Meeseek defines the interface for managing multiple programs with scheduling capabilities.
// It provides methods for adding programs, controlling execution, monitoring statistics,
// and managing the lifecycle of all managed programs.
type Meeseek interface {
	// AddProgram adds a new program to the meeseeks manager.
	// Returns an error if a program with the same name already exists.
	AddProgram(program.Program) error
	// Programs returns a sorted list of all managed program names with their command details.
	Programs() []string
	// Reload performs a hot reload of the meeseeks configuration with new programs.
	//
	// This method prioritizes data integrity over operational availability by holding an exclusive
	// lock for the entire reload duration. This means ALL other operations (Statistics, Status,
	// AddProgram, etc.) are blocked until reload completes.
	//
	// If an error happens while shutdown of previous programs we log the error but continue the reload process.
	Reload(context.Context, []program.Program, time.Duration)
	// Start begins execution of all managed programs according to their configuration.
	// One-time programs start immediately, while interval programs start their scheduling.
	Start(ctx context.Context)
	// Stop gracefully stops a specific program with the given timeout.
	// Returns an error if the program is not found or cannot be stopped gracefully.
	Stop(programName string, timeout time.Duration) error
	// Run executes a specific program immediately, regardless of its scheduling configuration.
	// Returns an error if the program is not found or is already running.
	Run(programName string) error
	// RunAsync executes a specific program asynchronously, regardless of its scheduling configuration.
	// Returns an error if the program is not found or is already running.
	RunAsync(programName string) error
	// Wait blocks until all programs have finished execution or the context is cancelled.
	// Returns an error if the context is cancelled before completion.
	Wait(ctx context.Context) error
	// SubscribeLogs return a channel to consume logs in real-time.
	// The caller is responsible for closing the context which ensure the channel is closed.
	SubscribeLogs(
		ctx context.Context,
		programName string,
		subscribeToPreviousLogs bool,
	) (<-chan program.LogLine, error)
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
	Successful  int    `json:"successful_runs"`
	Failed      int    `json:"failed_runs"`
	Retries     int    `json:"retries"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	Interval    string `json:"interval,omitempty"`
	LastRunAt   string `json:"last_run_at"`
	NextRunAt   string `json:"next_run,omitempty"`
}

type executionTrack struct {
	successful int
	failed     int
	retries    int
	lastRunAt  time.Time
}

type meeseek struct {
	programs       map[string]program.Program // Unified storage for all programs
	schedulerStops map[string]chan struct{}   // Only for scheduled programs
	executions     map[string]*executionTrack // execution tracking for each program
	wg             *sync.WaitGroup
	mu             sync.RWMutex
	logger         logger.Logger
}

// New creates a new Meeseek instance with the provided options.
// The returned instance is ready to have programs added and can be started.
func New(opts ...Option) Meeseek {
	m := &meeseek{
		wg:             &sync.WaitGroup{},
		programs:       make(map[string]program.Program),
		schedulerStops: make(map[string]chan struct{}),
		executions:     make(map[string]*executionTrack),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

func (m *meeseek) AddProgram(prog program.Program) error {
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

func (m *meeseek) Reload(ctx context.Context, programs []program.Program, deadline time.Duration) {
	if len(programs) == 0 {
		return
	}

	newPrograms := map[string]program.Program{}
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
			if equal(oldProgram, newProgram) {
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
			m.logger.Warn("error shutting down old programs while reloading", "error", err.Error())
		}
	}

	for _, name := range removeState {
		delete(m.executions, name)
	}

	m.programs = map[string]program.Program{}

	for name, prog := range newPrograms {
		m.programs[name] = prog
		if !slices.Contains(keepState, name) {
			m.executions[name] = &executionTrack{}
		}
	}

	m.mu.Unlock()

	m.Start(ctx)
}

func (m *meeseek) Start(ctx context.Context) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Start all programs
	for _, p := range m.programs {
		m.wg.Add(1)
		go func(prog program.Program) {
			m.runProgram(ctx, prog)
			m.wg.Done()
		}(p)
	}
}

func (m *meeseek) Stop(programName string, timeout time.Duration) error {
	m.mu.RLock()
	program, ok := m.programs[programName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("program %s not present", programName)
	}

	err := program.Shutdown(timeout)

	if program.Interval() > 0 {
		// Stop schedule loop
		m.mu.Lock()
		if stop, exists := m.schedulerStops[programName]; exists {
			closeOnce(stop)
			delete(m.schedulerStops, programName)
		}
		m.mu.Unlock()
	}

	return err
}

func (m *meeseek) Run(programName string) error {
	return m.runByName(programName, false)
}

func (m *meeseek) RunAsync(programName string) error {
	return m.runByName(programName, true)
}

func (m *meeseek) runByName(programName string, async bool) error {
	m.mu.RLock()
	prog, ok := m.programs[programName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("program %s not present", programName)
	}

	if prog.State() == program.StateRunning {
		return fmt.Errorf("program %s already running", programName)
	}

	if async {
		go m.runOneTimeProgram(context.Background(), prog)
	} else {
		m.runOneTimeProgram(context.Background(), prog)
	}

	return nil
}

func (m *meeseek) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return errors.New("context cancelled while waiting for programs to finalize")
	case <-done:
		return nil
	}
}

func (m *meeseek) SubscribeLogs(
	ctx context.Context,
	programName string,
	subscribeToPreviousLogs bool,
) (<-chan program.LogLine, error) {
	m.mu.RLock()
	prog, ok := m.programs[programName]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("program %s not present", programName)
	}

	return prog.SubscribeLogs(ctx, subscribeToPreviousLogs), nil
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
		closeOnce(stop)
	}
	m.schedulerStops = make(map[string]chan struct{})
}

// closeOnce closes ch if it is not already closed. Callers must hold m.mu so
// two goroutines cannot race to close the same channel.
func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
		// Channel already closed
	default:
		close(ch)
	}
}

func (m *meeseek) shutdown(timeout time.Duration) error {
	errs := make([]error, 0, len(m.programs))

	for _, program := range m.programs {
		errs = append(errs, program.Shutdown(timeout))
	}

	return errors.Join(errs...)
}

func (m *meeseek) runProgram(ctx context.Context, prog program.Program) {
	if prog.InitialDelay() > 0 {
		time.Sleep(prog.InitialDelay())
	}

	if prog.Interval() > 0 {
		m.runScheduledProgram(ctx, prog)
	} else {
		m.runOneTimeProgram(ctx, prog)
	}
}

func (m *meeseek) runOneTimeProgram(ctx context.Context, prog program.Program) {
	progState := prog.State()

	if progState == program.StateRunning {
		if m.logger != nil {
			m.logger.Warn(
				"skipping already running program",
				"program",
				prog.Name(),
			)
		}
		return
	}

	success := m.run(ctx, prog)
	retryAttempts := 0
	if !success {
		success, retryAttempts = m.retry(ctx, prog)
	}

	m.trackProgramCompletion(prog.Name(), success, retryAttempts)
}

func (m *meeseek) run(ctx context.Context, prog program.Program) bool {
	if m.logger != nil {
		m.logger.Info(
			"executing program",
			"program",
			prog.Name(),
		)
	}

	done, err := prog.Start(ctx)
	if err != nil {
		return false
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

	return prog.State() == program.StateFinished
}

func (m *meeseek) retry(ctx context.Context, prog program.Program) (bool, int) {
	retryDelay := prog.RetryDelay()

	var timer *time.Timer
	if retryDelay > 0 {
		timer = time.NewTimer(retryDelay)
		defer timer.Stop()
	}

	retryAttempts := 0
	for retryAttempts < prog.RetryCount() {
		// Wait for the retry delay (if any) before each attempt, bailing out
		// if the context is cancelled in the meantime. Without a delay both
		// select cases would be ready at once and a cancelled context would
		// only bail out half the time, so check it explicitly instead.
		if timer == nil {
			select {
			case <-ctx.Done():
				return false, retryAttempts
			default:
			}
		} else {
			select {
			case <-ctx.Done():
				return false, retryAttempts
			case <-timer.C:
			}
		}

		if m.logger != nil {
			m.logger.Info(
				"retrying program",
				"program",
				prog.Name(),
				"attempt",
				retryAttempts,
			)
		}

		done, err := prog.Start(ctx)
		if err == nil {
			<-done
		}
		retryAttempts++

		if err == nil && prog.State() == program.StateFinished {
			return true, retryAttempts
		}

		if timer != nil && retryAttempts < prog.RetryCount() {
			// The timer channel was drained by the select above, so Reset is safe.
			timer.Reset(retryDelay)
		}
	}

	return false, retryAttempts
}

func (m *meeseek) runScheduledProgram(ctx context.Context, prog program.Program) {
	programName := prog.Name()
	interval := prog.Interval()
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
	m.executeScheduledProgram(ctx, prog)

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

			// We strip the monotonic clock information using Round(0) because on some systems
			// the monotonic clock stops while the computer sleeps. That ensures now.Sub() is
			// accurate.
			now := time.Now().Round(0)
			strippedLastRunAt := exec.lastRunAt.Round(0)
			timeSinceLastRun := now.Sub(strippedLastRunAt)
			execute := timeSinceLastRun >= interval

			if execute {
				m.executeScheduledProgram(ctx, prog)
			}
		}
	}
}

// executeScheduledProgram handles execution of a scheduled program with consistent error handling.
func (m *meeseek) executeScheduledProgram(ctx context.Context, prog program.Program) {
	programName := prog.Name()

	// Check if we should stop before starting execution
	m.mu.RLock()
	stop, exists := m.schedulerStops[programName]
	m.mu.RUnlock()

	if !exists {
		// Stop channel was deleted, scheduler is shutting down
		return
	}

	innerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case <-ctx.Done():
			// Context cancelled while program was running
			cancel()
		case <-stop:
			// Program was stop while running
			cancel()
		case <-innerCtx.Done():
			// Program finished
			return
		}
	}()

	m.runOneTimeProgram(innerCtx, prog)
}

func (m *meeseek) collectProgramStatistics(
	prog program.Program,
	execRecord *executionTrack,
) Statistics {
	programName := prog.Name()
	progState := prog.State()

	stats := Statistics{
		ProgramName: programName,
		State:       program.StateToString[progState],
		Successful:  execRecord.successful,
		Failed:      execRecord.failed,
		Retries:     execRecord.retries,
		LastRunAt:   execRecord.lastRunAt.Format(time.DateTime),
	}

	interval := prog.Interval()

	if interval > 0 {
		stats.Interval = interval.String()
		stats.NextRunAt = execRecord.lastRunAt.Add(interval).Format(time.DateTime)
	}

	stats.Stderr = prog.Stderr()
	stats.Stdout = prog.Stdout()

	return stats
}

// trackProgramCompletion updates execution statistics when a program completes.
func (m *meeseek) trackProgramCompletion(programName string, success bool, retryAttempts int) {
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
	execRecord.retries += retryAttempts
	execRecord.lastRunAt = time.Now()
}

func equal(p1, p2 program.Program) bool {
	return p1.String() == p2.String()
}
