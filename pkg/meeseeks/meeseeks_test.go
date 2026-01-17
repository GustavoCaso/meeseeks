package meeseeks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/program"
)

func TestMeeseek_AddProgram(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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

func TestMeeseek_StartAndWait(t *testing.T) {
	t.Parallel()
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
			t.Parallel()

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

func TestMeeseek_StartInitialDelay(t *testing.T) {
	t.Parallel()
	initialDelay := time.Second * 1
	prog := program.New("echo1", "echo", program.Args("hello"), program.InitialDelay(initialDelay))

	m := New()

	err := m.AddProgram(prog)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}
	ctx := context.Background()

	start := time.Now()
	m.Start(ctx)

	err = m.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Wait() expected error but got none")
	}

	if elapsed < initialDelay {
		t.Fatalf("expected to meeseek to respect the initial delay option")
	}
}

func TestMeeseek_Statistic(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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

func TestMeeseek_Statistics(t *testing.T) {
	t.Parallel()
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
			t.Parallel()

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

			for name, stat := range stats {
				if !programNames[name] {
					t.Fatalf(
						"Statistics()[%s].ProgramName = %q, not found in expected programs",
						name,
						stat.ProgramName,
					)
				}

				if stat.Successful < 0 {
					t.Errorf("Statistics()[%s].Successful = %d, should be >= 0", name, stat.Successful)
				}

				if stat.Failed < 0 {
					t.Errorf("Statistics()[%s].Failed = %d, should be >= 0", name, stat.Failed)
				}

				if tt.runPrograms {
					if stat.Successful == 0 {
						t.Errorf("Statistics()[%s].Successful = 0, should be > 0 after running", name)
					}
					if stat.State == "finished" && stat.Successful == 0 {
						t.Errorf("Statistics()[%s]: finished program should have Successful > 0", name)
					}
					if stat.Stdout == "" {
						t.Errorf("Statistics()[%s].Stdout = %s, should not be empty", name, stat.Stdout)
					}
				}
			}
		})
	}
}

func TestMeeseek_LongRunningStatistics(t *testing.T) {
	t.Parallel()
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

	stat := stats["long-runner"]
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
	t.Parallel()
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
			t.Parallel()

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

			output := stat.Stdout

			if tt.expectOutput {
				if output == "" {
					t.Error("Expected Output to be non-empty")
				}
			} else if output != "" {
				t.Errorf("Expected Output empty for no-output program, got %s", stat.Stdout)
			}

			if tt.expectMultiline {
				lines := strings.Split(output, "\n")
				if len(lines) < 2 {
					t.Errorf("Expected Output lines >= 2 for multiline output, got %d", len(lines))
				}
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
	t.Parallel()
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
			t.Parallel()

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

				if stat.Stderr == "" && stat.State == "error" {
					t.Error("Expected Stderr to be non-empty for error state")
				}
			} else if stat.Successful == 0 {
				t.Error("Expected Succesful > 0 for succesful program")
			}
		})
	}
}

func TestMeeseek_ConcurrentAccess(t *testing.T) {
	t.Parallel()
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

	m.Start(t.Context())

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
	t.Parallel()
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
	t.Parallel()
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
			timeout:     1 * time.Second,
		},
		{
			name: "stop with interval",
			programs: []program.Program{
				program.New("test-stop-1", "sleep", program.Args("1"), program.Interval(20*time.Second)),
			},
			stopProgram: "test-stop-1",
			timeout:     2 * time.Second,
		},
		{
			name: "stop non-existing program",
			programs: []program.Program{
				program.New("test-stop-1", "sleep", program.Args("10")),
			},
			stopProgram: "non-existent",
			timeout:     time.Microsecond,
			wantErr:     true,
			errMsg:      "program non-existent not present",
		},
		{
			name: "stop with short timeout",
			programs: []program.Program{
				program.New("test-stop-3", "sleep", program.Args("10")),
			},
			stopProgram: "test-stop-3",
			timeout:     time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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
					"Program %s should be stopped but it still running.",
					tt.stopProgram,
				)
			}
		})
	}
}

func TestMeeseek_Stop_Same_Program_Multiple_Times(t *testing.T) {
	t.Parallel()
	m := New()
	prog := program.New("interval1", "echo", program.Args("test"), program.Interval(50*time.Millisecond))
	err := m.AddProgram(prog)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	m.Start(ctx)

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
	t.Parallel()
	tests := []struct {
		name        string
		programs    []program.Program
		waitTimeout time.Duration
		cancelAfter time.Duration
	}{
		{
			name: "single interval program context cancellation",
			programs: []program.Program{
				program.New("interval1", "echo", program.Args("test1"), program.Interval(50*time.Millisecond)),
			},
			waitTimeout: 2 * time.Second,
			cancelAfter: 200 * time.Millisecond,
		},
		{
			name: "multiple interval programs context cancellation",
			programs: []program.Program{
				program.New("interval1", "echo", program.Args("test1"), program.Interval(30*time.Millisecond)),
				program.New("interval2", "echo", program.Args("test2"), program.Interval(45*time.Millisecond)),
				program.New("interval3", "echo", program.Args("test3"), program.Interval(60*time.Millisecond)),
			},
			waitTimeout: 2 * time.Second,
			cancelAfter: 150 * time.Millisecond,
		},
		{
			name: "mixed regular and interval programs context cancellation",
			programs: []program.Program{
				program.New("regular1", "echo", program.Args("regular")),
				program.New("interval1", "echo", program.Args("interval"), program.Interval(50*time.Millisecond)),
				program.New("regular2", "sleep", program.Args("0.1")),
			},
			waitTimeout: 2 * time.Second,
			cancelAfter: 300 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := New()

			for _, prog := range tt.programs {
				err := m.AddProgram(prog)
				if err != nil {
					t.Fatalf("Failed to add program %s: %v", prog.Name(), err)
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
	t.Parallel()
	tests := []struct {
		name               string
		programs           []program.Program
		stopProgram        string
		stopAfter          time.Duration
		stopTimeout        time.Duration
		waitAfterStop      time.Duration
		expectWaitToReturn bool
	}{
		{
			name: "stop all program should exit",
			programs: []program.Program{
				program.New("interval1", "echo", program.Args("test1"), program.Interval(50*time.Millisecond)),
			},
			stopProgram:        "interval1",
			stopAfter:          100 * time.Millisecond,
			stopTimeout:        1 * time.Second,
			waitAfterStop:      200 * time.Millisecond,
			expectWaitToReturn: true,
		},
		{
			name: "stop all programs should not exit wait",
			programs: []program.Program{
				program.New("interval1", "echo", program.Args("test1"), program.Interval(50*time.Millisecond)),
				program.New("interval2", "echo", program.Args("test2"), program.Interval(50*time.Millisecond)),
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
			t.Parallel()
			m := New()

			for _, prog := range tt.programs {
				err := m.AddProgram(prog)
				if err != nil {
					t.Fatalf("Failed to add program %s: %v", prog.Name(), err)
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
	t.Parallel()
	tests := []struct {
		name              string
		programs          []program.Program
		shutdownAfter     time.Duration
		shutdownTimeout   time.Duration
		waitAfterShutdown time.Duration
	}{
		{
			name: "shutdown with single interval program",
			programs: []program.Program{
				program.New("interval1", "echo", program.Args("test"), program.Interval(50*time.Millisecond)),
			},
			shutdownAfter:     150 * time.Millisecond,
			shutdownTimeout:   2 * time.Second,
			waitAfterShutdown: 100 * time.Millisecond,
		},
		{
			name: "shutdown with multiple interval programs",
			programs: []program.Program{
				program.New("interval1", "echo", program.Args("test1"), program.Interval(30*time.Millisecond)),
				program.New("interval2", "echo", program.Args("test2"), program.Interval(45*time.Millisecond)),
				program.New("interval3", "echo", program.Args("test3"), program.Interval(60*time.Millisecond)),
			},
			shutdownAfter:     200 * time.Millisecond,
			shutdownTimeout:   2 * time.Second,
			waitAfterShutdown: 100 * time.Millisecond,
		},
		{
			name: "shutdown with mixed regular and interval programs",
			programs: []program.Program{
				program.New("regular1", "sleep", program.Args("2")),
				program.New("interval1", "echo", program.Args("interval"), program.Interval(40*time.Millisecond)),
				program.New("regular2", "sleep", program.Args("2")),
			},
			shutdownAfter:     150 * time.Millisecond,
			shutdownTimeout:   1 * time.Second,
			waitAfterShutdown: 100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := New()

			for _, prog := range tt.programs {
				err := m.AddProgram(prog)
				if err != nil {
					t.Fatalf("Failed to add program %s: %v", prog.Name(), err)
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
	t.Parallel()
	m := New()

	numPrograms := 5
	for i := range numPrograms {
		progName := fmt.Sprintf("interval-%d", i)
		duration := 30 + i*10
		interval := time.Duration(duration) * time.Millisecond
		prog := program.New(progName, "echo", program.Args("concurrent-test"), program.Interval(interval))
		err := m.AddProgram(prog)
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

func TestMeeseekReload(t *testing.T) {
	t.Parallel()
	interval50ms := 50 * time.Millisecond
	interval100ms := 100 * time.Millisecond

	tests := []struct {
		name                      string
		initialPrograms           []program.Program
		reloadPrograms            []program.Program
		expectedPrograms          []string
		verifyStatisticsPreserved bool
		preservedProgramName      string
		shortTimeout              bool
	}{
		{
			name: "basic reload with statistics preservation",
			initialPrograms: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
				program.New("test2", "echo", program.Args("world")),
				program.New("test3", "ls", program.Args("-la")),
			},
			reloadPrograms: []program.Program{
				program.New("test4", "echo", program.Args("foo")),
				program.New("test3", "ls", program.Args("-la")),
				program.New("test5", "echo", program.Args("bar")),
			},
			expectedPrograms: []string{
				"name: test3, command: ls, arguments: (-la)",
				"name: test4, command: echo, arguments: (foo)",
				"name: test5, command: echo, arguments: (bar)",
			},
			verifyStatisticsPreserved: true,
			preservedProgramName:      "test3",
		},
		{
			name:                      "reload to empty (early return)",
			initialPrograms:           []program.Program{program.New("test1", "echo", program.Args("hello"))},
			reloadPrograms:            []program.Program{},
			expectedPrograms:          []string{"name: test1, command: echo, arguments: (hello)"},
			verifyStatisticsPreserved: true,
			preservedProgramName:      "test1",
		},
		{
			name:            "reload from empty",
			initialPrograms: []program.Program{},
			reloadPrograms: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
			},
			expectedPrograms:          []string{"name: test1, command: echo, arguments: (hello)"},
			verifyStatisticsPreserved: false,
		},
		{
			name: "interval program statistics preservation",
			initialPrograms: []program.Program{
				program.New("interval1", "echo", program.Args("test1"), program.Interval(interval50ms)),
			},
			reloadPrograms: []program.Program{
				program.New("interval1", "echo", program.Args("test1"), program.Interval(interval50ms)),
				program.New("interval2", "echo", program.Args("test2"), program.Interval(interval100ms)),
			},
			expectedPrograms: []string{
				"name: interval1, command: echo, arguments: (test1), interval: 50ms",
				"name: interval2, command: echo, arguments: (test2), interval: 100ms",
			},
			verifyStatisticsPreserved: true,
			preservedProgramName:      "interval1",
		},
		{
			name: "interval change resets statistics",
			initialPrograms: []program.Program{
				program.New("prog1", "echo", program.Args("test"), program.Interval(interval50ms)),
			},
			reloadPrograms: []program.Program{
				program.New("prog1", "echo", program.Args("test"), program.Interval(interval100ms)),
			},
			expectedPrograms:          []string{"name: prog1, command: echo, arguments: (test), interval: 100ms"},
			verifyStatisticsPreserved: false,
		},
		{
			name: "short timeout handling",
			initialPrograms: []program.Program{
				program.New("slow-shutdown", "sleep", program.Args("2")),
			},
			reloadPrograms: []program.Program{
				program.New("fast-program", "echo", program.Args("hello")),
			},
			expectedPrograms: []string{"name: fast-program, command: echo, arguments: (hello)"},
			shortTimeout:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := New()
			ctx := t.Context()

			for _, p := range tt.initialPrograms {
				err := m.AddProgram(p)
				if err != nil {
					t.Fatalf("Failed to add initial program: %v", err)
				}
			}

			var oldStats map[string]Statistics
			var preservedOldStats Statistics

			// Start and capture statistics if needed
			m.Start(ctx)
			time.Sleep(200 * time.Millisecond) // Let programs execute

			if tt.verifyStatisticsPreserved {
				oldStats = m.Statistics()
				var exists bool
				preservedOldStats, exists = oldStats[tt.preservedProgramName]
				if !exists {
					t.Fatalf("Program %s not found in initial statistics", tt.preservedProgramName)
				}
				if preservedOldStats.Successful == 0 {
					t.Fatalf("Program %s should have successful executions before reload", tt.preservedProgramName)
				}
			}

			timeout := 2 * time.Second
			if tt.shortTimeout {
				timeout = 10 * time.Millisecond
			}
			m.Reload(ctx, tt.reloadPrograms, timeout)

			// Verify program list
			currentPrograms := m.Programs()
			if !reflect.DeepEqual(tt.expectedPrograms, currentPrograms) {
				t.Fatalf("Programs after reload: expected %+v, got %+v", tt.expectedPrograms, currentPrograms)
			}

			if tt.verifyStatisticsPreserved {
				newStats := m.Statistics()
				preservedNewStats, exists := newStats[tt.preservedProgramName]
				if !exists {
					t.Fatalf("Program %s not found in new statistics", tt.preservedProgramName)
				}
				if preservedNewStats.Successful < preservedOldStats.Successful {
					t.Fatalf("Statistics not preserved for %s: old successful=%d, new successful=%d",
						tt.preservedProgramName, preservedOldStats.Successful, preservedNewStats.Successful)
				}
			}

			err := m.Shutdown(1 * time.Second)
			if err != nil {
				t.Fatalf("Shutdown failed: %v", err)
			}
		})
	}
}

func TestMeeseekReload_BlockingOperations(t *testing.T) {
	t.Parallel()
	interval50ms := 50 * time.Millisecond
	initialPrograms := []program.Program{
		program.New("long-runner", "sleep", program.Args("2"), program.Interval(interval50ms)),
	}
	reloadPrograms := []program.Program{
		program.New("new-prog", "echo", program.Args("hello")),
	}

	m := New()
	ctx := t.Context()

	for _, p := range initialPrograms {
		err := m.AddProgram(p)
		if err != nil {
			t.Fatalf("Failed to add initial program: %v", err)
		}
	}

	m.Start(ctx)
	time.Sleep(200 * time.Millisecond) // Let programs execute

	reloadCalled := make(chan bool, 1)
	reloadDone := make(chan bool, 1)
	statsDone := make(chan bool, 1)

	go func() {
		reloadCalled <- true
		m.Reload(ctx, reloadPrograms, 2*time.Second)
		reloadDone <- true
	}()

	<-reloadCalled

	go func() {
		_ = m.Statistics()
		statsDone <- true
	}()

	select {
	case <-statsDone:
		t.Fatal("Statistics should be blocked and not complete before reload")
	case <-reloadDone:
		// Reload finished first (expected)
	case <-time.After(4 * time.Second):
		t.Fatal("Test timed out")
	}

	err := m.Shutdown(1 * time.Second)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}

func TestMeeseekReload_WaitDoNotExistWhileReloading(t *testing.T) {
	t.Parallel()
	interval50ms := 50 * time.Millisecond
	initialPrograms := []program.Program{
		program.New("long-runner", "sleep", program.Args("2"), program.Interval(interval50ms)),
	}
	reloadPrograms := []program.Program{
		program.New("new-prog", "echo", program.Args("hello")),
	}

	m := New()
	ctx := t.Context()

	for _, p := range initialPrograms {
		err := m.AddProgram(p)
		if err != nil {
			t.Fatalf("Failed to add initial program: %v", err)
		}
	}

	m.Start(ctx)
	time.Sleep(200 * time.Millisecond) // Let programs execute

	waitCalled := make(chan bool, 1)
	waitDone := make(chan bool, 1)
	reloadDone := make(chan bool, 1)

	go func() {
		waitCalled <- true
		m.Wait(ctx)
		waitDone <- true
	}()

	<-waitCalled

	go func() {
		m.Reload(ctx, reloadPrograms, 2*time.Second)
		reloadDone <- true
	}()

	select {
	case <-waitDone:
		t.Fatal("Wait returned while calling shutdown. This should not happen")
	case <-reloadDone:
	// Wait is still waiting (correctly)
	case <-time.After(3 * time.Second):
		t.Fatal("Test timed out")
	}

	err := m.Shutdown(1 * time.Second)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}

func TestMeeseek_SubscribeLogs(t *testing.T) {
	t.Parallel()

	t.Run("subscribe to existing program", func(t *testing.T) {
		t.Parallel()
		m := New()

		prog := program.New("test-logs", "bash",
			program.Args("-c", "echo 'line1'; echo 'line2'; echo 'error' >&2"),
		)
		err := m.AddProgram(prog)
		if err != nil {
			t.Fatalf("Failed to add program: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		_, err = m.SubscribeLogs(ctx, "test-logs", true)
		if err != nil {
			t.Fatalf("Failed to subscribe to logs: %v", err)
		}
	})

	t.Run("subscribe to nonexistent program", func(t *testing.T) {
		t.Parallel()
		m := New()

		ctx := context.Background()
		_, err := m.SubscribeLogs(ctx, "nonexistent", true)
		if err == nil {
			t.Fatal("Expected error for nonexistent program, got none")
		}
		if !strings.Contains(err.Error(), "program nonexistent not present") {
			t.Fatalf("Expected error to contain 'program nonexistent not present', got %q", err.Error())
		}
	})
}

func TestMeeseek_Run(t *testing.T) {
	t.Parallel()

	interval := 100 * time.Millisecond

	tests := []struct {
		name          string
		setupPrograms []program.Program
		runProgram    string
		startMesseks  bool
		wantErr       bool
		errMsg        string
	}{
		{
			name: "run existing program",
			setupPrograms: []program.Program{
				program.New("test-echo", "echo", program.Args("hello")),
				program.New("test-sleep", "sleep", program.Args("0.01")),
			},
			runProgram: "test-echo",
			wantErr:    false,
		},
		{
			name: "run nonexistent program",
			setupPrograms: []program.Program{
				program.New("test-echo", "echo", program.Args("hello")),
			},
			runProgram: "nonexistent",
			wantErr:    true,
			errMsg:     "program nonexistent not present",
		},
		{
			name: "run already running program",
			setupPrograms: []program.Program{
				program.New("long-sleep", "sleep", program.Args("10")),
			},
			runProgram:   "long-sleep",
			startMesseks: true,
			wantErr:      true,
			errMsg:       "program long-sleep already running",
		},
		{
			name: "run scheduled program",
			setupPrograms: []program.Program{
				program.New("scheduled", "echo", program.Args("hello"), program.Interval(interval)),
			},
			runProgram: "scheduled",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := New()

			for _, prog := range tt.setupPrograms {
				err := m.AddProgram(prog)
				if err != nil {
					t.Fatalf("Failed to add program: %v", err)
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if tt.startMesseks {
				m.Start(ctx)

				// Give programs time to start
				time.Sleep(100 * time.Millisecond)
			}

			err := m.Run(tt.runProgram)

			defer func() {
				shutdownErr := m.Shutdown(1 * time.Second)
				if shutdownErr != nil {
					t.Logf("Shutdown error (non-fatal): %v", shutdownErr)
				}
			}()

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected error but got none")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("Expected error message to contain %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			stats, statErr := m.Statistic(tt.runProgram)
			if statErr != nil {
				t.Fatalf("Failed to get statistics for %s: %v", tt.runProgram, statErr)
			}

			if stats.Successful == 0 {
				t.Fatalf("Program %s should have been executed but shows no runs", tt.runProgram)
			}
		})
	}
}

func TestMeeseekRetry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		program         program.Program
		expectedSuccess int
		expectedFailed  int
		expectedRetries int
		expectedState   string
	}{
		{
			name: "program succeeds on first attempt - no retries",
			program: program.New("success-first",
				"echo",
				program.Args("hello"),
				program.RetryCount(3),
			),
			expectedSuccess: 1,
			expectedFailed:  0,
			expectedRetries: 0,
			expectedState:   "finished",
		},
		{
			name: "program with zero retries succeeds",
			program: program.New("no-retry-success",
				"echo",
				program.Args("hello"),
			),
			expectedSuccess: 1,
			expectedFailed:  0,
			expectedRetries: 0,
			expectedState:   "finished",
		},
		{
			name: "program fails all attempts including retries",
			program: program.New("fail-all",
				"sh",
				program.Args("-c", "exit 1"),
				program.RetryCount(3),
			),
			expectedSuccess: 0,
			expectedFailed:  1,
			expectedRetries: 3,
			expectedState:   "error",
		},
		{
			name: "program with zero retries fails",
			program: program.New("no-retry-fail",
				"sh",
				program.Args("-c", "exit 1"),
			),
			expectedSuccess: 0,
			expectedFailed:  1,
			expectedRetries: 0,
			expectedState:   "error",
		},
		{
			name: "nonexistent command fails all retries",
			program: program.New("nonexistent-cmd",
				"nonexistent-command-12345",
				program.Args(),
				program.RetryCount(2),
			),
			expectedSuccess: 0,
			expectedFailed:  1,
			expectedRetries: 2,
			expectedState:   "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := New()
			err := m.AddProgram(tt.program)
			if err != nil {
				t.Fatalf("Failed to add program: %v", err)
			}

			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			m.Start(ctx)
			err = m.Wait(ctx)
			if err != nil {
				t.Fatalf("Wait() unexpected error = %v", err)
			}

			stats, err := m.Statistic(tt.program.Name())
			if err != nil {
				t.Fatalf("Statistic() unexpected error = %v", err)
			}

			if stats.Successful != tt.expectedSuccess {
				t.Errorf("Successful = %d, want %d", stats.Successful, tt.expectedSuccess)
			}
			if stats.Failed != tt.expectedFailed {
				t.Errorf("Failed = %d, want %d", stats.Failed, tt.expectedFailed)
			}
			if stats.Retries != tt.expectedRetries {
				t.Errorf("Retries = %d, want %d", stats.Retries, tt.expectedRetries)
			}
			if stats.State != tt.expectedState {
				t.Errorf("State = %q, want %q", stats.State, tt.expectedState)
			}
		})
	}
}

func TestMeeseek_RetryEventualSuccess(t *testing.T) {
	t.Parallel()

	// Create a file that gets deleted after first read to simulate eventual success
	tempDir := t.TempDir()
	markerFile := filepath.Join(tempDir, "fail-once")
	err := os.WriteFile(markerFile, []byte("fail"), 0600)
	if err != nil {
		t.Fatalf("Failed to create marker file: %v", err)
	}

	prog := program.New("eventual-success",
		"sh",
		program.Args("-c", fmt.Sprintf("if [ -f %s ]; then rm %s; exit 1; else exit 0; fi", markerFile, markerFile)),
		program.RetryCount(3),
		program.RetryDelay(10*time.Millisecond),
	)

	m := New()
	err = m.AddProgram(prog)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	m.Start(ctx)
	err = m.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait() unexpected error = %v", err)
	}

	stats, err := m.Statistic("eventual-success")
	if err != nil {
		t.Fatalf("Statistic() unexpected error = %v", err)
	}

	if stats.Successful != 1 {
		t.Errorf("Successful = %d, want 1 (should succeed after retries)", stats.Successful)
	}
	if stats.Failed != 0 {
		t.Errorf("Failed = %d, want 0", stats.Failed)
	}
	if stats.Retries < 1 {
		t.Errorf("Retries = %d, want >= 1 (should have retried at least once)", stats.Retries)
	}
	if stats.State != "finished" {
		t.Errorf("State = %q, want %q", stats.State, "finished")
	}
}

func TestMeeseek_RetryDelay(t *testing.T) {
	t.Parallel()

	retryDelay := 100 * time.Millisecond
	prog := program.New("retry-with-delay",
		"sh",
		program.Args("-c", "exit 1"),
		program.RetryCount(2),
		program.RetryDelay(retryDelay),
	)

	m := New()
	err := m.AddProgram(prog)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	start := time.Now()
	m.Start(ctx)
	_ = m.Wait(ctx)
	elapsed := time.Since(start)

	// Should take at least retryCount * retryDelay
	expectedMinDuration := time.Duration(prog.RetryCount()) * retryDelay
	if elapsed < expectedMinDuration {
		t.Errorf("Elapsed time = %v, want >= %v (should respect retry delay)", elapsed, expectedMinDuration)
	}

	stats, err := m.Statistic("retry-with-delay")
	if err != nil {
		t.Fatalf("Statistic() unexpected error = %v", err)
	}

	if stats.Retries != prog.RetryCount() {
		t.Errorf("Retries = %d, want %d", stats.Retries, prog.RetryCount())
	}
}

func TestMeeseek_RetryContextCancellation(t *testing.T) {
	t.Parallel()

	prog := program.New("retry-cancel",
		"sh",
		program.Args("-c", "exit 1"),
		program.RetryCount(10),
		program.RetryDelay(200*time.Millisecond),
	)

	m := New()
	err := m.AddProgram(prog)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	m.Start(ctx)
	_ = m.Wait(ctx)

	stats, err := m.Statistic("retry-cancel")
	if err != nil {
		t.Fatalf("Statistic() unexpected error = %v", err)
	}

	// Should not complete all 10 retries due to context cancellation
	if stats.Retries >= 10 {
		t.Errorf("Retries = %d, should be < 10 due to context cancellation", stats.Retries)
	}
}
