package meeseeks

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/program"
)

func TestMeeseek_AddProgram(t *testing.T) {
	tests := []struct {
		name     string
		programs []program.Program
		wantErr  bool
		errMsg   string
	}{
		{
			name: "add single program",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
			},
		},
		{
			name: "add multiple programs",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
				program.New("test2", "echo", program.Args("world")),
				program.New("test3", "ls", program.Args("-la")),
			},
		},
		{
			name: "add duplicate program name",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
				program.New("test1", "ls", program.Args("-la")),
			},
			wantErr: true,
			errMsg:  "duplicated test1 program",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			var err error
			for _, prog := range tt.programs {
				err = m.AddProgram(prog)
				if err != nil && !tt.wantErr {
					t.Fatalf("AddProgram() unexpected error = %v", err)
					return
				}
				if err != nil && tt.wantErr {
					break
				}
			}

			if tt.wantErr {
				if err == nil {
					t.Fatalf("AddProgram() expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("AddProgram() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("AddProgram() unexpected error = %v", err)
			}
		})
	}
}

func TestMeeseek_Statistic(t *testing.T) {
	tests := []struct {
		name          string
		programs      []program.Program
		programName   string
		runPrograms   bool
		expectedState string
		wantErr       bool
		errMsg        string
	}{
		{
			name: "statistic of existing program not started",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
			},
			programName:   "test1",
			runPrograms:   false,
			expectedState: "not started",
		},
		{
			name: "statistic of existing program finished",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
			},
			programName:   "test1",
			runPrograms:   true,
			expectedState: "finished",
		},
		{
			name: "statistic of nonexistent program",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
			},
			programName: "nonexistent",
			wantErr:     true,
			errMsg:      "program nonexistent not present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			for _, prog := range tt.programs {
				err := m.AddProgram(prog)
				if err != nil {
					t.Fatalf("Failed to add program: %v", err)
				}
			}

			if tt.runPrograms {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()

				m.Start(ctx)
				err := m.Wait(ctx)
				if err != nil {
					t.Fatalf("Failed to wait for programs: %v", err)
				}
			}

			statistic, err := m.Statistic(tt.programName)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Statistic() expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("Statistic() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("Statistic() unexpected error = %v", err)
				return
			}

			if statistic.ProgramName != tt.programName {
				t.Errorf("Statistic().ProgramName = %q, want %q", statistic.ProgramName, tt.programName)
			}

			if statistic.State != tt.expectedState {
				t.Errorf("Statistic().State = %q, want %q", statistic.State, tt.expectedState)
			}

			if tt.runPrograms {
				if statistic.State == "finished" && statistic.Successful == 0 {
					t.Error("Statistic().Successful should be > 0 for finished program")
				}
			} else {
				if statistic.Successful+statistic.Failed != 0 {
					t.Errorf("Statistic().Successful + statistic().Failed = %d, want 0 for not started program", statistic.Successful+statistic.Failed)
				}
			}
		})
	}
}

func TestMeeseek_StartAndWait(t *testing.T) {
	tests := []struct {
		name     string
		programs []program.Program
		timeout  time.Duration
		wantErr  bool
	}{
		{
			name: "start and wait for simple programs",
			programs: []program.Program{
				program.New("echo1", "echo", program.Args("hello")),
				program.New("echo2", "echo", program.Args("world")),
			},
			timeout: 5 * time.Second,
		},
		{
			name: "timeout before completion",
			programs: []program.Program{
				program.New("slow", "sleep", program.Args("10")),
			},
			timeout: 100 * time.Millisecond,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			for _, prog := range tt.programs {
				err := m.AddProgram(prog)
				if err != nil {
					t.Fatalf("Failed to add program: %v", err)
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), tt.timeout)
			defer cancel()

			m.Start(ctx)

			err := m.Wait(ctx)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Wait() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Wait() unexpected error = %v", err)
			}
		})
	}
}

func TestMeeseek_Statistics(t *testing.T) {
	tests := []struct {
		name          string
		programs      []program.Program
		expectedCount int
		runPrograms   bool
	}{
		{
			name:          "no programs",
			programs:      []program.Program{},
			expectedCount: 0,
		},
		{
			name: "programs without running",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
				program.New("test2", "echo", program.Args("world")),
			},
			expectedCount: 2,
			runPrograms:   false,
		},
		{
			name: "programs after running",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
				program.New("test2", "echo", program.Args("world")),
			},
			expectedCount: 2,
			runPrograms:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			for _, prog := range tt.programs {
				err := m.AddProgram(prog)
				if err != nil {
					t.Fatalf("Failed to add program: %v", err)
				}
			}

			if tt.runPrograms {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()

				m.Start(ctx)
				err := m.Wait(ctx)
				if err != nil {
					t.Fatalf("Failed to wait for programs: %v", err)
				}
			}

			stats := m.Statistics()

			if len(stats) != tt.expectedCount {
				t.Fatalf("Statistics() returned %d statistics, want %d", len(stats), tt.expectedCount)
			}

			programNames := make(map[string]bool)
			for _, prog := range tt.programs {
				programNames[prog.Name()] = true
			}

			for i, stat := range stats {
				if !programNames[stat.ProgramName] {
					t.Fatalf("Statistics()[%d].ProgramName = %q, not found in expected programs", i, stat.ProgramName)
				}

				if stat.Successful < 0 {
					t.Errorf("Statistics()[%d].Successful = %d, should be >= 0", i, stat.Successful)
				}
				if stat.Failed < 0 {
					t.Errorf("Statistics()[%d].Failed = %d, should be >= 0", i, stat.Failed)
				}
				if stat.TotalOutputLines < 0 {
					t.Errorf("Statistics()[%d].TotalOutputLines = %d, should be >= 0", i, stat.TotalOutputLines)
				}

				validStates := []string{"not started", "idle", "running", "finished", "error"}
				validState := false
				for _, validS := range validStates {
					if stat.State == validS {
						validState = true
						break
					}
				}
				if !validState {
					t.Errorf("Statistics()[%d].State = %q, should be one of %v", i, stat.State, validStates)
				}

				if tt.runPrograms {
					if stat.Successful == 0 {
						t.Errorf("Statistics()[%d].Successful = 0, should be > 0 after running", i)
					}
					if stat.State == "finished" && stat.Successful == 0 {
						t.Errorf("Statistics()[%d]: finished program should have Successful > 0", i)
					}
				}
			}
		})
	}
}

func TestMeeseek_LongRunningStatistics(t *testing.T) {
	m := New()

	prog := program.New("long-runner", "sleep", program.Args("5"))
	err := m.AddProgram(prog)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	m.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	stats := m.Statistics()
	if len(stats) != 1 {
		t.Fatalf("Expected 1 statistic, got %d", len(stats))
	}

	stat := stats[0]
	t.Logf("Statistics: %+v", stat)

	if stat.State != "running" {
		t.Errorf("Expected State='running', got %q", stat.State)
	}

	individualStat, err := m.Statistic("long-runner")
	if err != nil {
		t.Fatalf("Failed to get individual statistic: %v", err)
	}

	if individualStat.State != stat.State {
		t.Errorf("Individual statistic State mismatch: got %q, want %q", individualStat.State, stat.State)
	}
}

func TestMeeseek_StatisticsWithOutput(t *testing.T) {
	tests := []struct {
		name            string
		programCmd      string
		programArgs     []string
		expectOutput    bool
		expectMultiline bool
	}{
		{
			name:         "program with single line output",
			programCmd:   "echo",
			programArgs:  []string{"hello world"},
			expectOutput: true,
		},
		{
			name:            "program with multiline output",
			programCmd:      "printf",
			programArgs:     []string{"line1\nline2\nline3"},
			expectOutput:    true,
			expectMultiline: true,
		},
		{
			name:         "program with no output",
			programCmd:   "true",
			programArgs:  []string{},
			expectOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			prog := program.New("output-test", tt.programCmd, program.Args(tt.programArgs...))
			err := m.AddProgram(prog)
			if err != nil {
				t.Fatalf("Failed to add program: %v", err)
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			m.Start(ctx)
			err = m.Wait(ctx)
			if err != nil {
				t.Fatalf("Failed to wait for programs: %v", err)
			}

			stat, err := m.Statistic("output-test")
			if err != nil {
				t.Fatalf("Failed to get statistic: %v", err)
			}

			if tt.expectOutput {
				if stat.LastOutput == "" {
					t.Error("Expected LastOutput to be non-empty")
				}
				if stat.TotalOutputLines == 0 {
					t.Error("Expected TotalOutputLines to be > 0")
				}
				if tt.expectMultiline && stat.TotalOutputLines < 2 {
					t.Errorf("Expected TotalOutputLines >= 2 for multiline output, got %d", stat.TotalOutputLines)
				}
			} else if stat.TotalOutputLines > 1 {
				t.Errorf("Expected TotalOutputLines <= 1 for no-output program, got %d", stat.TotalOutputLines)
			}

			if stat.Successful != 1 {
				t.Errorf("Expected Successful = 1, got %d", stat.Successful)
			}
			if stat.Failed != 0 {
				t.Errorf("Expected Failed = 0, got %d", stat.Failed)
			}
			if stat.State != "finished" {
				t.Errorf("Expected State = 'finished', got %q", stat.State)
			}
		})
	}
}

func TestMeeseek_FailingProgramStatistics(t *testing.T) {
	tests := []struct {
		name        string
		programCmd  string
		programArgs []string
		expectError bool
	}{
		{
			name:        "program with non-zero exit code",
			programCmd:  "sh",
			programArgs: []string{"-c", "exit 1"},
			expectError: true,
		},
		{
			name:        "nonexistent program",
			programCmd:  "nonexistent-command-12345",
			programArgs: []string{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			prog := program.New("failing-test", tt.programCmd, program.Args(tt.programArgs...))
			err := m.AddProgram(prog)
			if err != nil {
				t.Fatalf("Failed to add program: %v", err)
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			m.Start(ctx)
			_ = m.Wait(ctx)

			stat, err := m.Statistic("failing-test")
			if err != nil {
				t.Fatalf("Failed to get statistic: %v", err)
			}

			if tt.expectError {
				if stat.Failed == 0 {
					t.Error("Expected Failed > 0 for failing program")
				}
				if stat.State != "error" && stat.State != "finished" {
					t.Errorf("Expected State to be 'error' or 'finished', got %q", stat.State)
				}

				if stat.LastError == "" && stat.State == "error" {
					t.Error("Expected LastError to be non-empty for error state")
				}
			} else if stat.Successful == 0 {
				t.Error("Expected Succesful > 0 for succesful program")
			}
		})
	}
}

func TestMeeseek_IntervalPrograms(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		waitTime time.Duration
		minRuns  int
		maxRuns  int
	}{
		{
			name:     "interval program runs multiple times",
			interval: 50 * time.Millisecond,
			waitTime: 200 * time.Millisecond,
			minRuns:  3,
			maxRuns:  6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			prog := program.New("interval-test", "echo",
				program.Args("interval"))

			err := m.AddProgram(prog, tt.interval)
			if err != nil {
				t.Fatalf("Failed to add program: %v", err)
			}

			ctx, cancel := context.WithTimeout(t.Context(), tt.waitTime+time.Second)
			defer cancel()

			m.Start(ctx)

			time.Sleep(tt.waitTime)

			stats := m.Statistics()
			if len(stats) != 1 {
				t.Fatalf("Expected 1 statistic, got %d", len(stats))
			}

			cancel()
		})
	}
}

func TestMeeseek_ConcurrentAccess(t *testing.T) {
	m := New()

	numPrograms := 10
	results := make(chan error, numPrograms)

	for i := range numPrograms {
		go func(id int) {
			progName := "concurrent-" + string(rune('0'+id))
			prog := program.New(progName, "echo", program.Args("test"))
			err := m.AddProgram(prog)
			results <- err
		}(i)
	}

	for i := range numPrograms {
		err := <-results
		if err != nil {
			t.Fatalf("Failed to add program %d: %v", i, err)
		}
	}

	stats := m.Statistics()
	if len(stats) != numPrograms {
		t.Fatalf("Expected %d programs, got %d", numPrograms, len(stats))
	}

	statusResults := make(chan error, numPrograms)
	for i := range numPrograms {
		go func(id int) {
			progName := "concurrent-" + string(rune('0'+id))
			_, err := m.Statistic(progName)
			statusResults <- err
		}(i)
	}

	for i := range numPrograms {
		err := <-statusResults
		if err != nil {
			t.Fatalf("Failed to get statictics for program %d: %v", i, err)
		}
	}
}

func TestMeeseek_ContextCancellation(t *testing.T) {
	m := New()

	prog := program.New("cancelable", "sleep", program.Args("10"))
	err := m.AddProgram(prog)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	m.Start(ctx)

	err = m.Wait(ctx)
	if err == nil {
		t.Fatalf("Wait() should return error when context is cancelled")
	}

	expectedMsg := "context cancelled"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Fatalf("Wait() error should contain %q, got %q", expectedMsg, err.Error())
	}
}

func TestMeeseek_Stop(t *testing.T) {
	tests := []struct {
		name        string
		programs    []program.Program
		stopProgram string
		timeout     time.Duration
		wantErr     bool
		errMsg      string
	}{
		{
			name: "stop existing program",
			programs: []program.Program{
				program.New("test-stop-1", "sleep", program.Args("10")),
				program.New("test-stop-2", "sleep", program.Args("10")),
			},
			stopProgram: "test-stop-1",
			timeout:     5 * time.Second,
		},
		{
			name: "stop non-existing program",
			programs: []program.Program{
				program.New("test-stop-1", "sleep", program.Args("10")),
			},
			stopProgram: "non-existent",
			timeout:     5 * time.Second,
			wantErr:     true,
			errMsg:      "program non-existent not present",
		},
		{
			name: "stop with short timeout",
			programs: []program.Program{
				program.New("test-stop-3", "sleep", program.Args("10")),
			},
			stopProgram: "test-stop-3",
			timeout:     1 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			for _, prog := range tt.programs {
				err := m.AddProgram(prog)
				if err != nil {
					t.Fatalf("Failed to add program: %v", err)
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			m.Start(ctx)

			time.Sleep(100 * time.Millisecond)

			err := m.Stop(tt.stopProgram, tt.timeout)

			if tt.wantErr {
				if err == nil {
					t.Fatal("Stop() expected error but got none")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("Stop() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("Stop() unexpected error = %v", err)
			}

			stat, err := m.Statistic(tt.stopProgram)
			if err != nil {
				t.Fatalf("Failed to get statistic after stop: %v", err)
			}

			if stat.State == "running" {
				t.Fatalf(
					"Program %s should be stopped but it still running",
					tt.stopProgram,
				)
			}
		})
	}
}

func TestMeeseek_Stop_Same_Program_Multiple_Times(t *testing.T) {
	m := New()
	prog := program.New("interval1", "echo", program.Args("test"))
	err := m.AddProgram(prog, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	m.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	err = m.Stop("interval1", 1*time.Second)
	if err != nil {
		t.Errorf("First stop failed: %v", err)
	}

	err = m.Stop("interval1", 1*time.Second)
	if err != nil {
		t.Errorf("Second stop failed: %v", err)
	}
}

func TestMeeseek_WaitWithIntervalPrograms_ContextCancellation(t *testing.T) {
	tests := []struct {
		name        string
		programs    []programConfig
		waitTimeout time.Duration
		cancelAfter time.Duration
	}{
		{
			name: "single interval program context cancellation",
			programs: []programConfig{
				{name: "interval1", cmd: "echo", args: []string{"test"}, interval: 50 * time.Millisecond},
			},
			waitTimeout: 2 * time.Second,
			cancelAfter: 200 * time.Millisecond,
		},
		{
			name: "multiple interval programs context cancellation",
			programs: []programConfig{
				{name: "interval1", cmd: "echo", args: []string{"test1"}, interval: 30 * time.Millisecond},
				{name: "interval2", cmd: "echo", args: []string{"test2"}, interval: 40 * time.Millisecond},
				{name: "interval3", cmd: "echo", args: []string{"test3"}, interval: 60 * time.Millisecond},
			},
			waitTimeout: 2 * time.Second,
			cancelAfter: 150 * time.Millisecond,
		},
		{
			name: "mixed regular and interval programs context cancellation",
			programs: []programConfig{
				{name: "regular1", cmd: "echo", args: []string{"regular"}},
				{name: "interval1", cmd: "echo", args: []string{"interval"}, interval: 50 * time.Millisecond},
				{name: "regular2", cmd: "sleep", args: []string{"0.1"}},
			},
			waitTimeout: 2 * time.Second,
			cancelAfter: 300 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			for _, pc := range tt.programs {
				prog := program.New(pc.name, pc.cmd, program.Args(pc.args...))
				if pc.interval > 0 {
					err := m.AddProgram(prog, pc.interval)
					if err != nil {
						t.Fatalf("Failed to add interval program %s: %v", pc.name, err)
					}
				} else {
					err := m.AddProgram(prog)
					if err != nil {
						t.Fatalf("Failed to add regular program %s: %v", pc.name, err)
					}
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), tt.waitTimeout)
			defer cancel()

			m.Start(ctx)

			go func() {
				time.Sleep(tt.cancelAfter)
				cancel()
			}()

			start := time.Now()
			err := m.Wait(ctx)
			duration := time.Since(start)

			expectedErrorMessage := "context cancelled while waiting for programs to finalize"

			if err == nil {
				t.Fatal("Wait() expected error but got none")
			}
			if !strings.Contains(err.Error(), expectedErrorMessage) {
				t.Fatalf("Wait() error = %q, want error containing %q", err.Error(), expectedErrorMessage)
			}

			expectedMax := tt.cancelAfter + (200 * time.Millisecond)
			if duration > expectedMax {
				t.Errorf("Wait() took %v, expected to return within %v after cancellation", duration, expectedMax)
			}

			stats := m.Statistics()
			if len(stats) != len(tt.programs) {
				t.Errorf("Expected %d statistics after cancellation, got %d", len(tt.programs), len(stats))
			}
		})
	}
}

func TestMeeseek_WaitWithIntervalPrograms_Stop(t *testing.T) {
	tests := []struct {
		name               string
		programs           []programConfig
		stopProgram        string
		stopAfter          time.Duration
		stopTimeout        time.Duration
		waitAfterStop      time.Duration
		expectWaitToReturn bool
	}{
		{
			name: "stop all program should exit",
			programs: []programConfig{
				{name: "interval1", cmd: "echo", args: []string{"test1"}, interval: 50 * time.Millisecond},
			},
			stopProgram:        "interval1",
			stopAfter:          100 * time.Millisecond,
			stopTimeout:        1 * time.Second,
			waitAfterStop:      200 * time.Millisecond,
			expectWaitToReturn: true,
		},
		{
			name: "stop all programs should not exit wait",
			programs: []programConfig{
				{name: "interval1", cmd: "echo", args: []string{"test1"}, interval: 50 * time.Millisecond},
				{name: "interval2", cmd: "echo", args: []string{"test2"}, interval: 60 * time.Millisecond},
			},
			stopProgram:        "interval1",
			stopAfter:          100 * time.Millisecond,
			stopTimeout:        1 * time.Second,
			waitAfterStop:      100 * time.Millisecond,
			expectWaitToReturn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			for _, pc := range tt.programs {
				prog := program.New(pc.name, pc.cmd, program.Args(pc.args...))
				if pc.interval > 0 {
					err := m.AddProgram(prog, pc.interval)
					if err != nil {
						t.Fatalf("Failed to add interval program %s: %v", pc.name, err)
					}
				} else {
					err := m.AddProgram(prog)
					if err != nil {
						t.Fatalf("Failed to add regular program %s: %v", pc.name, err)
					}
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			m.Start(ctx)

			go func() {
				time.Sleep(tt.stopAfter)
				err := m.Stop(tt.stopProgram, tt.stopTimeout)
				if err != nil {
					t.Errorf("Failed to stop program %s: %v", tt.stopProgram, err)
				}
			}()

			waitDone := make(chan error, 1)
			go func() {
				waitDone <- m.Wait(ctx)
			}()

			timeout := tt.stopAfter + tt.waitAfterStop
			select {
			case err := <-waitDone:
				if !tt.expectWaitToReturn {
					t.Fatal("Wait() returned unexpectedly")
				}
				if err != nil {
					t.Errorf("Wait() returned unexpected error: %v", err)
				}
			case <-time.After(timeout):
				if tt.expectWaitToReturn {
					t.Fatal("Wait() should have returned but didn't")
				}

				cancel()
				<-waitDone
			}
		})
	}
}

func TestMeeseek_WaitWithIntervalPrograms_Shutdown(t *testing.T) {
	tests := []struct {
		name              string
		programs          []programConfig
		shutdownAfter     time.Duration
		shutdownTimeout   time.Duration
		waitAfterShutdown time.Duration
	}{
		{
			name: "shutdown with single interval program",
			programs: []programConfig{
				{name: "interval1", cmd: "echo", args: []string{"test"}, interval: 50 * time.Millisecond},
			},
			shutdownAfter:     150 * time.Millisecond,
			shutdownTimeout:   2 * time.Second,
			waitAfterShutdown: 100 * time.Millisecond,
		},
		{
			name: "shutdown with multiple interval programs",
			programs: []programConfig{
				{name: "interval1", cmd: "echo", args: []string{"test1"}, interval: 30 * time.Millisecond},
				{name: "interval2", cmd: "echo", args: []string{"test2"}, interval: 45 * time.Millisecond},
				{name: "interval3", cmd: "echo", args: []string{"test3"}, interval: 60 * time.Millisecond},
			},
			shutdownAfter:     200 * time.Millisecond,
			shutdownTimeout:   2 * time.Second,
			waitAfterShutdown: 100 * time.Millisecond,
		},
		{
			name: "shutdown with mixed regular and interval programs",
			programs: []programConfig{
				{name: "regular1", cmd: "sleep", args: []string{"2"}},
				{name: "interval1", cmd: "echo", args: []string{"interval"}, interval: 40 * time.Millisecond},
				{name: "regular2", cmd: "sleep", args: []string{"2"}},
			},
			shutdownAfter:     150 * time.Millisecond,
			shutdownTimeout:   1 * time.Second,
			waitAfterShutdown: 100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			for _, pc := range tt.programs {
				prog := program.New(pc.name, pc.cmd, program.Args(pc.args...))
				if pc.interval > 0 {
					err := m.AddProgram(prog, pc.interval)
					if err != nil {
						t.Fatalf("Failed to add interval program %s: %v", pc.name, err)
					}
				} else {
					err := m.AddProgram(prog)
					if err != nil {
						t.Fatalf("Failed to add regular program %s: %v", pc.name, err)
					}
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			m.Start(ctx)

			go func() {
				time.Sleep(tt.shutdownAfter)
				err := m.Shutdown(tt.shutdownTimeout)
				if err != nil {
					t.Errorf("Shutdown returned error: %v", err)
				}
			}()

			start := time.Now()
			err := m.Wait(ctx)
			duration := time.Since(start)

			if err != nil {
				t.Errorf("Wait() returned unexpected error after shutdown: %v", err)
			}

			expectedMax := tt.shutdownAfter + tt.shutdownTimeout + tt.waitAfterShutdown
			if duration > expectedMax {
				t.Errorf("Wait() took %v, expected to return within %v after shutdown", duration, expectedMax)
			}
		})
	}
}

func TestMeeseek_IntervalPrograms_ConcurrentOperations(t *testing.T) {
	m := New()

	numPrograms := 5
	for i := range numPrograms {
		progName := fmt.Sprintf("interval-%d", i)
		prog := program.New(progName, "echo", program.Args("concurrent-test"))
		duration := 30 + i*10
		interval := time.Duration(duration) * time.Millisecond
		err := m.AddProgram(prog, interval)
		if err != nil {
			t.Fatalf("Failed to add interval program %s: %v", progName, err)
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	m.Start(ctx)

	operations := 20
	results := make(chan error, operations)

	for i := range operations / 2 {
		go func(id int) {
			time.Sleep(time.Duration(id*5) * time.Millisecond)
			stats := m.Statistics()
			if len(stats) != numPrograms {
				results <- fmt.Errorf("concurrent stats check %d: expected %d programs, got %d", id, numPrograms, len(stats))
				return
			}
			results <- nil
		}(i)
	}

	for i := range operations / 2 {
		go func(id int) {
			time.Sleep(time.Duration(id*3) * time.Millisecond)
			progName := fmt.Sprintf("interval-%d", id%numPrograms)
			_, err := m.Statistic(progName)
			results <- err
		}(i)
	}

	for i := range operations {
		err := <-results
		if err != nil {
			t.Errorf("Concurrent operation %d failed: %v", i, err)
		}
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		err := m.Shutdown(1 * time.Second)
		if err != nil {
			t.Errorf("Shutdown failed: %v", err)
		}
	}()

	err := m.Wait(ctx)
	if err != nil && !strings.Contains(err.Error(), "context cancelled") {
		t.Errorf("Wait after shutdown returned unexpected error: %v", err)
	}
}

type programConfig struct {
	name     string
	cmd      string
	args     []string
	interval time.Duration
}
