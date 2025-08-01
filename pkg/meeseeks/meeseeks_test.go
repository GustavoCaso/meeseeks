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
					t.Errorf("AddProgram() unexpected error = %v", err)
					return
				}
				if err != nil && tt.wantErr {
					break
				}
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("AddProgram() expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("AddProgram() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("AddProgram() unexpected error = %v", err)
			}
		})
	}
}

func TestMeeseek_Statistic(t *testing.T) {
	tests := []struct {
		name        string
		programs    []program.Program
		statusQuery string
		wantErr     bool
		errMsg      string
	}{
		{
			name: "statistic of existing program",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
			},
			statusQuery: "test1",
		},
		{
			name: "statistic of nonexistent program",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
			},
			statusQuery: "nonexistent",
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

			statistic, err := m.Statistic(tt.statusQuery)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Status() expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Statistic() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("Statistic() unexpected error = %v", err)
				return
			}

			if statistic.ProgramName != tt.statusQuery {
				t.Errorf("Statistic() returned incorrect statistic")
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
					t.Errorf("Wait() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Wait() unexpected error = %v", err)
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
				t.Errorf("Statistics() returned %d statistics, want %d", len(stats), tt.expectedCount)
			}

			// Verify each statistic has a valid program name
			programNames := make(map[string]bool)
			for _, prog := range tt.programs {
				programNames[prog.Name()] = true
			}

			for i, stat := range stats {
				if !programNames[stat.ProgramName] {
					t.Errorf("Statistics()[%d].ProgramName = %q, not found in expected programs", i, stat.ProgramName)
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
				program.Args("interval"),
				program.Interval(tt.interval))

			err := m.AddProgram(prog)
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

			runs := stats[0].TotalRuns
			if runs < tt.minRuns || runs > tt.maxRuns {
				t.Errorf("Expected %d-%d runs, got %d", tt.minRuns, tt.maxRuns, runs)
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
			t.Errorf("Failed to add program %d: %v", i, err)
		}
	}

	// Verify all programs were added
	stats := m.Statistics()
	if len(stats) != numPrograms {
		t.Errorf("Expected %d programs, got %d", numPrograms, len(stats))
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
			t.Errorf("Failed to get statictics for program %d: %v", i, err)
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
		t.Errorf("Wait() should return error when context is cancelled")
	}

	expectedMsg := "context cancelled"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Wait() error should contain %q, got %q", expectedMsg, err.Error())
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
					t.Errorf("Stop() expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Stop() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("Stop() unexpected error = %v", err)
			}

			// Verify the program is stopped by checking its status
			if !tt.wantErr {
				stat, err := m.Statistic(tt.stopProgram)
				if err != nil {
					t.Errorf("Failed to get statistic after stop: %v", err)
					return
				}
				// The program should have terminated
				if stat.Running > 0 {
					t.Errorf(
						"Program %s should be stopped but %d instances are still running",
						tt.stopProgram,
						stat.Running,
					)
				}
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
