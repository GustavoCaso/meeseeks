package meeseeks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/logger"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

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
}

type executionTrack struct {
	successful int
	failed     int
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
	defer ticker.Stop()
	defer m.wg.Done()

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
				if m.logger != nil {
					m.logger.Error("interval program failed", "program", prog.Name(), "error", err.Error())
				}
				m.trackProgramCompletion(programName, false)
				return
			}

			select {
			case <-done:
				// Program execution completed normally
				if m.logger != nil {
					m.logger.Info(
						"interval program executed",
						"program",
						prog.Name(),
						"state",
						program.StateToString[prog.State()],
					)
				}
				m.trackProgramCompletion(programName, prog.State() == program.StateFinished)
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

	statistics := make([]Statistics, 0, len(m.programs))

	for name, info := range m.programs {
		execRecord := m.executions[name]
		statistics = append(statistics, m.collectProgramStatistics(info, execRecord))
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
func (m *meeseek) collectProgramStatistics(info *ProgramInfo, execRecord *executionTrack) Statistics {
	prog := info.Program
	programName := prog.Name()
	progState := prog.State()

	stats := Statistics{
		ProgramName: programName,
		State:       program.StateToString[progState],
		Successful:  execRecord.successful,
		Failed:      execRecord.failed,
	}

	if info.Interval != nil {
		stats.Interval = info.Interval.String()
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
