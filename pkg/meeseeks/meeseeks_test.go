package meeseeks

import (
	"context"
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
		{
			name: "statistic of async program",
			programs: []program.Program{
				program.New("async-test", "echo", program.Args("async"), program.Async()),
			},
			programName:   "async-test",
			runPrograms:   true,
			expectedState: "finished",
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

			// Validate statistic content
			if statistic.ProgramName != tt.programName {
				t.Errorf("Statistic().ProgramName = %q, want %q", statistic.ProgramName, tt.programName)
			}

			if statistic.State != tt.expectedState {
				t.Errorf("Statistic().State = %q, want %q", statistic.State, tt.expectedState)
			}

			// Validate run counts based on whether programs were executed
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
			name: "start program with async option",
			programs: []program.Program{
				program.New("async-echo", "echo", program.Args("async"), program.Async()),
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

			// Verify each statistic has a valid program name
			programNames := make(map[string]bool)
			for _, prog := range tt.programs {
				programNames[prog.Name()] = true
			}

			for i, stat := range stats {
				if !programNames[stat.ProgramName] {
					t.Fatalf("Statistics()[%d].ProgramName = %q, not found in expected programs", i, stat.ProgramName)
				}

				// Validate statistics content consistency
				if stat.Successful < 0 {
					t.Errorf("Statistics()[%d].Successful = %d, should be >= 0", i, stat.Successful)
				}
				if stat.Failed < 0 {
					t.Errorf("Statistics()[%d].Failed = %d, should be >= 0", i, stat.Failed)
				}
				if stat.TotalOutputLines < 0 {
					t.Errorf("Statistics()[%d].TotalOutputLines = %d, should be >= 0", i, stat.TotalOutputLines)
				}

				// Validate state consistency
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

				// For ran programs, validate execution metrics
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

			// Let interval program run for specified time
			time.Sleep(tt.waitTime)

			// Check statistics
			stats := m.Statistics()
			if len(stats) != 1 {
				t.Fatalf("Expected 1 statistic, got %d", len(stats))
			}

			cancel() // Cancel context to stop interval program
		})
	}
}

func TestMeeseek_ConcurrentAccess(t *testing.T) {
	m := New()

	// Add programs concurrently
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

	// Wait for all programs to be added
	for i := range numPrograms {
		err := <-results
		if err != nil {
			t.Fatalf("Failed to add program %d: %v", i, err)
		}
	}

	// Verify all programs were added
	stats := m.Statistics()
	if len(stats) != numPrograms {
		t.Fatalf("Expected %d programs, got %d", numPrograms, len(stats))
	}

	// Test concurrent statisitics queries
	statusResults := make(chan error, numPrograms)
	for i := range numPrograms {
		go func(id int) {
			progName := "concurrent-" + string(rune('0'+id))
			_, err := m.Statistic(progName)
			statusResults <- err
		}(i)
	}

	// Wait for all status queries
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

// Benchmark meeseek operations.
func BenchmarkMeeseek_AddProgram(b *testing.B) {
	m := New()

	b.ResetTimer()
	for i := range b.N {
		progName := "bench-" + string(rune('0'+(i%10)))
		prog := program.New(progName, "echo", program.Args("benchmark"))
		m.AddProgram(prog)
	}
}

func BenchmarkMeeseek_Statistics(b *testing.B) {
	m := New()

	// Add some programs
	for i := range 10 {
		progName := "bench-" + string(rune('0'+i))
		prog := program.New(progName, "echo", program.Args("benchmark"))
		m.AddProgram(prog)
	}

	b.ResetTimer()
	for range b.N {
		m.Statistics()
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

			// Start programs
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			m.Start(ctx)

			// Give programs time to start
			time.Sleep(100 * time.Millisecond)

			// Test Stop method
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
			// The program should have terminated
			if stat.State == "running" {
				t.Fatalf(
					"Program %s should be stopped but it still running",
					tt.stopProgram,
				)
			}
		})
	}
}

func BenchmarkMeeseek_Statistic(b *testing.B) {
	m := New()

	prog := program.New("bench-statistic", "echo", program.Args("benchmark"))
	m.AddProgram(prog)

	b.ResetTimer()
	for range b.N {
		m.Statistic("bench-status")
	}
}

// Test long-running program statistics while program is still running.
func TestMeeseek_LongRunningStatistics(t *testing.T) {
	m := New()

	// Add a long-running program
	prog := program.New("long-runner", "sleep", program.Args("5"))
	err := m.AddProgram(prog)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}

	// Start the program but don't wait for completion
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	m.Start(ctx)

	// Give the program time to start
	time.Sleep(100 * time.Millisecond)

	// Check statistics while program is still running
	stats := m.Statistics()
	if len(stats) != 1 {
		t.Fatalf("Expected 1 statistic, got %d", len(stats))
	}

	stat := stats[0]
	t.Logf("Statistics: %+v", stat)

	if stat.State != "running" {
		t.Errorf("Expected State='running', got %q", stat.State)
	}

	// Test individual statistic query while running
	individualStat, err := m.Statistic("long-runner")
	if err != nil {
		t.Fatalf("Failed to get individual statistic: %v", err)
	}

	if individualStat.State != stat.State {
		t.Errorf("Individual statistic State mismatch: got %q, want %q", individualStat.State, stat.State)
	}
}

// Test statistics for programs with output
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
			programCmd:   "true", // command that produces no output
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

			// Run the program
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			m.Start(ctx)
			err = m.Wait(ctx)
			if err != nil {
				t.Fatalf("Failed to wait for programs: %v", err)
			}

			// Check statistics for output information
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
			} else {
				// For programs with no output, these should be minimal
				if stat.TotalOutputLines > 1 {
					t.Errorf("Expected TotalOutputLines <= 1 for no-output program, got %d", stat.TotalOutputLines)
				}
			}

			// Validate basic execution statistics
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

			// Run the program (expect it to fail)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			m.Start(ctx)
			err = m.Wait(ctx) // This might return an error or success depending on implementation

			// Check statistics regardless of Wait() result
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
				// LastError should contain error information
				if stat.LastError == "" && stat.State == "error" {
					t.Error("Expected LastError to be non-empty for error state")
				}
			} else {
				if stat.Successful == 0 {
					t.Error("Expected Succesful > 0 for succesful program")
				}
			}
		})
	}
}

// Test empty meeseek statistics
func TestMeeseek_EmptyStatistics(t *testing.T) {
	m := New()

	// Test Statistics() on empty meeseek
	stats := m.Statistics()
	if len(stats) != 0 {
		t.Errorf("Expected empty statistics, got %d items", len(stats))
	}

	// Test Statistic() on empty meeseek
	_, err := m.Statistic("nonexistent")
	if err == nil {
		t.Error("Expected error for Statistic() on empty meeseek")
	}
	expectedMsg := "program nonexistent not present"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Error message should contain %q, got %q", expectedMsg, err.Error())
	}

	// Add program but don't run it
	prog := program.New("idle-test", "echo", program.Args("test"))
	err = m.AddProgram(prog)
	if err != nil {
		t.Fatalf("Failed to add program: %v", err)
	}

	// Check statistics for non-started program
	stat, err := m.Statistic("idle-test")
	if err != nil {
		t.Fatalf("Failed to get statistic: %v", err)
	}

	if stat.State != "not started" {
		t.Errorf("Expected State = 'not started', got %q", stat.State)
	}
	if stat.Successful != 0 {
		t.Errorf("Expected Successful = 0, got %d", stat.Successful)
	}
	if stat.Failed != 0 {
		t.Errorf("Expected Failed = 0, got %d", stat.Failed)
	}
}
