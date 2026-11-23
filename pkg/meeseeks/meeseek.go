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

// supervision is one launch of a program, covering its retries and schedule.
// Stopping is two steps. Closing stop prevents any further run, while the
// running process gets its full shutdown timeout. Cancelling ctx afterwards
// terminates a run that started just as the stop arrived. Cancelling first
// would apply the program's kill grace instead of the requested timeout.
type supervision struct {
	stop   chan struct{}
	ctx    context.Context //nolint:containedctx // Scoped to a single launch
	cancel context.CancelFunc
}

// ended reports whether no further run may start.
func (s *supervision) ended() bool {
	return isClosed(s.stop) || s.ctx.Err() != nil
}

// wait blocks for d and reports whether the launch may still run.
func (s *supervision) wait(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-s.ctx.Done():
	case <-s.stop:
	case <-timer.C:
	}
	return !s.ended()
}

type meeseek struct {
	programs     map[string]program.Program // Unified storage for all programs
	supervisions map[string][]*supervision  // Active launches of each program
	executions   map[string]*executionTrack // execution tracking for each program
	wg           *sync.WaitGroup
	mu           sync.RWMutex
	logger       logger.Logger

	schedulerTick time.Duration // How often schedules check whether a run is due
}

// New creates a new Meeseek instance with the provided options.
// The returned instance is ready to have programs added and can be started.
func New(opts ...Option) Meeseek {
	m := &meeseek{
		wg:           &sync.WaitGroup{},
		programs:     make(map[string]program.Program),
		supervisions: make(map[string][]*supervision),
		executions:   make(map[string]*executionTrack),

		schedulerTick: intervalCheckDuration,
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

	stopping := m.beginStopAllLocked()

	err := m.shutdown(deadline)
	cancelSupervisions(stopping)
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

	m.startLocked(ctx)
	m.mu.Unlock()
}

func (m *meeseek) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startLocked(ctx)
}

// startLocked launches every program. Callers must hold m.mu.
func (m *meeseek) startLocked(ctx context.Context) {
	for _, p := range m.programs {
		launch := m.superviseLocked(ctx, p.Name())
		m.wg.Add(1)
		go func(prog program.Program) {
			defer m.wg.Done()
			defer m.release(prog.Name(), launch)
			m.runProgram(launch, prog)
		}(p)
	}
}

func (m *meeseek) Stop(programName string, timeout time.Duration) error {
	m.mu.Lock()
	prog, ok := m.programs[programName]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("program %s not present", programName)
	}
	stopping := m.beginStopLocked(programName)
	m.mu.Unlock()

	err := prog.Shutdown(timeout)
	cancelSupervisions(stopping)

	return err
}

func (m *meeseek) Run(programName string) error {
	return m.runByName(programName, false)
}

func (m *meeseek) RunAsync(programName string) error {
	return m.runByName(programName, true)
}

func (m *meeseek) runByName(programName string, async bool) error {
	m.mu.Lock()
	prog, ok := m.programs[programName]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("program %s not present", programName)
	}

	if prog.State() == program.StateRunning {
		m.mu.Unlock()
		return fmt.Errorf("program %s already running", programName)
	}

	launch := m.superviseLocked(context.Background(), programName)
	m.mu.Unlock()

	run := func() {
		defer m.release(programName, launch)
		m.runOneTimeProgram(launch, prog)
	}

	if async {
		go run()
	} else {
		run()
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
	m.mu.Lock()
	stopping := m.beginStopAllLocked()
	m.mu.Unlock()

	err := m.shutdown(timeout)
	cancelSupervisions(stopping)

	return err
}

// superviseLocked registers a new launch of a program. Callers must hold m.mu.
func (m *meeseek) superviseLocked(ctx context.Context, programName string) *supervision {
	launchCtx, cancel := context.WithCancel(ctx)
	launch := &supervision{
		stop:   make(chan struct{}),
		ctx:    launchCtx,
		cancel: cancel,
	}
	m.supervisions[programName] = append(m.supervisions[programName], launch)
	return launch
}

// release removes a finished launch. A stop or reload may already have removed
// it, and a newer launch under the same name must be left in place.
func (m *meeseek) release(programName string, launch *supervision) {
	launch.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	remaining := slices.DeleteFunc(m.supervisions[programName], func(s *supervision) bool {
		return s == launch
	})
	if len(remaining) == 0 {
		delete(m.supervisions, programName)
		return
	}
	m.supervisions[programName] = remaining
}

// beginStopLocked prevents further runs of a program and returns its launches
// so they can be cancelled after shutdown. Callers must hold m.mu.
func (m *meeseek) beginStopLocked(programName string) []*supervision {
	launches := m.supervisions[programName]
	delete(m.supervisions, programName)

	for _, launch := range launches {
		close(launch.stop)
	}
	return launches
}

// beginStopAllLocked is beginStopLocked for every program.
func (m *meeseek) beginStopAllLocked() []*supervision {
	var stopping []*supervision
	for _, programName := range slices.Collect(maps.Keys(m.supervisions)) {
		stopping = append(stopping, m.beginStopLocked(programName)...)
	}
	return stopping
}

func cancelSupervisions(launches []*supervision) {
	for _, launch := range launches {
		launch.cancel()
	}
}

// isClosed reports whether ch has been closed.
func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (m *meeseek) shutdown(timeout time.Duration) error {
	errs := make([]error, 0, len(m.programs))

	for _, program := range m.programs {
		errs = append(errs, program.Shutdown(timeout))
	}

	return errors.Join(errs...)
}

func (m *meeseek) runProgram(launch *supervision, prog program.Program) {
	if delay := prog.InitialDelay(); delay > 0 && !launch.wait(delay) {
		return
	}

	if prog.Interval() > 0 {
		m.runScheduledProgram(launch, prog)
	} else {
		m.runOneTimeProgram(launch, prog)
	}
}

func (m *meeseek) runOneTimeProgram(launch *supervision, prog program.Program) {
	// A launch stopped before its first run never ran, so it is not a failure.
	if launch.ended() {
		return
	}

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

	success := m.run(launch.ctx, prog)
	retryAttempts := 0
	if !success {
		success, retryAttempts = m.retry(launch, prog)
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

func (m *meeseek) retry(launch *supervision, prog program.Program) (bool, int) {
	retryDelay := prog.RetryDelay()

	retryAttempts := 0
	for retryAttempts < prog.RetryCount() {
		// Wait for the retry delay (if any) before each attempt, bailing out
		// if the launch is stopped in the meantime.
		if !launch.wait(retryDelay) {
			return false, retryAttempts
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

		done, err := prog.Start(launch.ctx)
		if err == nil {
			<-done
		}
		retryAttempts++

		if err == nil && prog.State() == program.StateFinished {
			return true, retryAttempts
		}
	}

	return false, retryAttempts
}

func (m *meeseek) runScheduledProgram(launch *supervision, prog program.Program) {
	programName := prog.Name()
	interval := prog.Interval()
	ticker := time.NewTicker(m.schedulerTick)

	defer ticker.Stop()

	// Execute the program immediately first
	m.executeScheduledProgram(launch, prog)

	// Main interval loop
	for {
		select {
		case <-launch.ctx.Done():
			return
		case <-launch.stop:
			return
		case <-ticker.C:
			// select picks at random when a tick and stop are both ready.
			if launch.ended() {
				return
			}

			lastRunAt, tracked := m.lastRunAt(programName)
			if !tracked {
				// A reload removed this program.
				return
			}

			// We strip the monotonic clock information using Round(0) because on some systems
			// the monotonic clock stops while the computer sleeps. That ensures now.Sub() is
			// accurate.
			now := time.Now().Round(0)
			strippedLastRunAt := lastRunAt.Round(0)
			timeSinceLastRun := now.Sub(strippedLastRunAt)
			execute := timeSinceLastRun >= interval

			if execute {
				m.executeScheduledProgram(launch, prog)
			}
		}
	}
}

// lastRunAt returns when a program last completed, and whether it is still tracked.
func (m *meeseek) lastRunAt(programName string) (time.Time, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	execRecord, ok := m.executions[programName]
	if !ok {
		return time.Time{}, false
	}
	return execRecord.lastRunAt, true
}

// executeScheduledProgram handles execution of a scheduled program with consistent error handling.
func (m *meeseek) executeScheduledProgram(launch *supervision, prog program.Program) {
	// Check if we should stop before starting execution
	if launch.ended() {
		return
	}

	m.runOneTimeProgram(launch, prog)
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
