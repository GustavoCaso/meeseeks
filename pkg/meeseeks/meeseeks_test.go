package meeseeks

import (
	"bytes"
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

func TestMeeseek_Status(t *testing.T) {
	tests := []struct {
		name        string
		programs    []program.Program
		statusQuery string
		wantErr     bool
		errMsg      string
	}{
		{
			name: "status of existing program",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
			},
			statusQuery: "test1",
		},
		{
			name: "status of nonexistent program",
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

			status, err := m.Status(tt.statusQuery)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Status() expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Status() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("Status() unexpected error = %v", err)
				return
			}

			if status == "" {
				t.Errorf("Status() returned empty string")
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

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
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
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

func TestMeeseek_Results(t *testing.T) {
	tests := []struct {
		name         string
		programs     []program.Program
		runPrograms  bool
		expectedText []string
	}{
		{
			name: "results without running",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
			},
			runPrograms:  false,
			expectedText: []string{"Execution still in progress", "test1"},
		},
		{
			name: "results after running",
			programs: []program.Program{
				program.New("test1", "echo", program.Args("hello")),
			},
			runPrograms:  true,
			expectedText: []string{"Total Execution Time", "test1"},
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
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				m.Start(ctx)
				err := m.Wait(ctx)
				if err != nil {
					t.Fatalf("Failed to wait for programs: %v", err)
				}
			}

			var buf bytes.Buffer
			m.Results(&buf)
			output := buf.String()

			for _, expectedText := range tt.expectedText {
				if !strings.Contains(output, expectedText) {
					t.Errorf("Results() output missing expected text %q. Full output:\n%s", expectedText, output)
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

			ctx, cancel := context.WithTimeout(context.Background(), tt.waitTime+time.Second)
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

	for i := 0; i < numPrograms; i++ {
		go func(id int) {
			progName := "concurrent-" + string(rune('0'+id))
			prog := program.New(progName, "echo", program.Args("test"))
			err := m.AddProgram(prog)
			results <- err
		}(i)
	}

	// Wait for all programs to be added
	for i := 0; i < numPrograms; i++ {
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

	// Test concurrent status queries
	statusResults := make(chan error, numPrograms)
	for i := 0; i < numPrograms; i++ {
		go func(id int) {
			progName := "concurrent-" + string(rune('0'+id))
			_, err := m.Status(progName)
			statusResults <- err
		}(i)
	}

	// Wait for all status queries
	for i := 0; i < numPrograms; i++ {
		err := <-statusResults
		if err != nil {
			t.Errorf("Failed to get status for program %d: %v", i, err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
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

func TestMeeseek_EmptyMeeseek(t *testing.T) {
	m := New()

	// Test operations on empty meeseek
	ctx := context.Background()
	m.Start(ctx)

	err := m.Wait(ctx)
	if err != nil {
		t.Errorf("Wait() on empty meeseek should not error, got %v", err)
	}

	stats := m.Statistics()
	if len(stats) != 0 {
		t.Errorf("Statistics() on empty meeseek should return empty slice, got %d items", len(stats))
	}

	_, err = m.Status("nonexistent")
	if err == nil {
		t.Errorf("Status() on empty meeseek should return error")
	}

	var buf bytes.Buffer
	m.Results(&buf)
	output := buf.String()
	if !strings.Contains(output, "Meeseeks Execution Summary") {
		t.Errorf("Results() should contain header even for empty meeseek")
	}
}

// Benchmark meeseek operations
func BenchmarkMeeseek_AddProgram(b *testing.B) {
	m := New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		progName := "bench-" + string(rune('0'+(i%10)))
		prog := program.New(progName, "echo", program.Args("benchmark"))
		m.AddProgram(prog)
	}
}

func BenchmarkMeeseek_Statistics(b *testing.B) {
	m := New()

	// Add some programs
	for i := 0; i < 10; i++ {
		progName := "bench-" + string(rune('0'+i))
		prog := program.New(progName, "echo", program.Args("benchmark"))
		m.AddProgram(prog)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Statistics()
	}
}

func BenchmarkMeeseek_Status(b *testing.B) {
	m := New()

	prog := program.New("bench-status", "echo", program.Args("benchmark"))
	m.AddProgram(prog)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Status("bench-status")
	}
}
