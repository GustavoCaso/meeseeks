package meeseeks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/program"
)

type Meeseek interface {
	AddProgram(prog program.Program, interval ...time.Duration) error
	Start(ctx context.Context)
	Stop(programName string, timeout time.Duration) error
	Wait(ctx context.Context) error
	Statistic(program string) (ExecutionRecord, error)
	Statistics() []ExecutionRecord
	Shutdown(timeout time.Duration) error
}

// ProgramInfo holds program metadata for unified storage.
type ProgramInfo struct {
	Program  program.Program
	Interval *time.Duration // nil for regular programs, non-nil for scheduled
}

// ExecutionRecord tracks individual program execution statistics.
type ExecutionRecord struct {
	ProgramName       string `json:"program_name"`
	State             string `json:"state"`
	TotalRuns         int    `json:"total_runs"`
	Successful        int    `json:"successful_runs"`
	Failed            int    `json:"failed_runs"`
	Running           int    `json:"running"`
	TotalOutputLines  int    `json:"total_output_lines"`
	LastSuccessfulRun int    `json:"last_successful_run"`
	LastError         string `json:"last_error"`
	LastOutput        string `json:"last_output"`
}

type meeseek struct {
	startTime      time.Time
	endTime        time.Time
	programs       map[string]*ProgramInfo     // Unified storage for all programs
	schedulerStops map[string]chan struct{}    // Only for scheduled programs
	executions     map[string]*ExecutionRecord // Statistics for each program
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
				defer m.wg.Done()
				done, err := prog.Start(ctx)
				if err != nil {
					slog.Error("failed to start program", "program", prog.Name(), "error", err.Error())
					m.trackProgramStart(prog.Name())
					m.trackProgramCompletion(prog.Name(), false)
					return
				}
				// Track that the program started
				m.trackProgramStart(prog.Name())
				<-done
				// Track completion success based on final state
				success := prog.State() == program.StateFinished
				m.trackProgramCompletion(prog.Name(), success)
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
				m.trackProgramStart(programName)
				m.trackProgramCompletion(programName, false)
				return
			}
			// Track that the program started
			m.trackProgramStart(programName)

			select {
			case <-done:
				// Program execution completed normally
				success := prog.State() == program.StateFinished
				m.trackProgramCompletion(programName, success)
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

func (m *meeseek) Statistic(programName string) (ExecutionRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.programs[programName]
	if !ok {
		return ExecutionRecord{}, fmt.Errorf("program %s not present", programName)
	}

	return m.collectProgramStatistics(info), nil
}

func (m *meeseek) Statistics() []ExecutionRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statistics := make([]ExecutionRecord, 0, len(m.programs))

	for _, info := range m.programs {
		statistics = append(statistics, m.collectProgramStatistics(info))
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

// collectProgramStatistics gathers statistics from program state and execution records.
func (m *meeseek) collectProgramStatistics(info *ProgramInfo) ExecutionRecord {
	prog := info.Program
	programName := prog.Name()
	progState := prog.State()

	// Get current state
	var state string
	switch progState {
	case program.StateFinished:
		state = "finished"
	case program.StateIdle:
		state = "idle"
	case program.StateError:
		state = "error"
	case program.StateRunning:
		state = "running"
	case program.StateNotStarted:
		state = "not started"
	}

	// Get or initialize execution record
	execRecord, exists := m.executions[programName]
	if !exists {
		execRecord = &ExecutionRecord{
			LastSuccessfulRun: -1,
		}
		m.executions[programName] = execRecord
	}

	// If program is running but we don't have it tracked as running, sync the state
	// This handles cases where statistics are requested before tracking methods are called
	if progState == program.StateRunning && execRecord.Running == 0 {
		execRecord.Running = 1
		// If we don't have any runs tracked yet, assume this is the first run
		if execRecord.TotalRuns == 0 {
			execRecord.TotalRuns = 1
		}
	} else if progState != program.StateRunning {
		execRecord.Running = 0
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
		execRecord.LastOutput = lastOutput
	}
	if lastError != "" {
		execRecord.LastError = lastError
	}

	// Count output lines
	output := prog.Output()
	if output != "" {
		execRecord.TotalOutputLines = strings.Count(output, "\n")
	}

	return ExecutionRecord{
		ProgramName:       programName,
		State:             state,
		TotalRuns:         execRecord.TotalRuns,
		Successful:        execRecord.Successful,
		Failed:            execRecord.Failed,
		Running:           execRecord.Running,
		TotalOutputLines:  execRecord.TotalOutputLines,
		LastSuccessfulRun: execRecord.LastSuccessfulRun,
		LastError:         execRecord.LastError,
		LastOutput:        execRecord.LastOutput,
	}
}

// trackProgramStart updates execution statistics when a program starts.
func (m *meeseek) trackProgramStart(programName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	execRecord, exists := m.executions[programName]
	if !exists {
		execRecord = &ExecutionRecord{
			LastSuccessfulRun: -1,
		}
		m.executions[programName] = execRecord
	}

	execRecord.TotalRuns++
	execRecord.Running = 1
}

// trackProgramCompletion updates execution statistics when a program completes.
func (m *meeseek) trackProgramCompletion(programName string, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	execRecord, exists := m.executions[programName]
	if !exists {
		// This shouldn't happen if trackProgramStart was called first
		execRecord = &ExecutionRecord{
			LastSuccessfulRun: -1,
		}
		m.executions[programName] = execRecord
	}

	execRecord.Running = 0
	if success {
		execRecord.Successful++
		execRecord.LastSuccessfulRun = execRecord.TotalRuns - 1 // 0-based indexing
	} else {
		execRecord.Failed++
	}
}

func New() Meeseek {
	return &meeseek{
		wg:             &sync.WaitGroup{},
		programs:       make(map[string]*ProgramInfo),
		schedulerStops: make(map[string]chan struct{}),
		executions:     make(map[string]*ExecutionRecord),
	}
}
