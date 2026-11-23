package meeseeks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/program"
)

const (
	// reloadRaceInstances is how many managers run a reload race test. The
	// failures depend on goroutine scheduling, so one attempt can miss them.
	reloadRaceInstances = 24
	// testSchedulerTick replaces the one-second production tick so the reload
	// race tests can run many instances quickly.
	testSchedulerTick = 5 * time.Millisecond
	// testInterval is the schedule of programs in the reload race tests.
	testInterval = 20 * time.Millisecond
	// schedulerTickWait is long enough for a scheduler tick to become pending
	// while the program is still running.
	schedulerTickWait = 3 * testSchedulerTick
	// recordRunAndSleep appends to the file in $1, then stays running.
	recordRunAndSleep = `echo run >> "$1"; exec sleep 30`
)

func withSchedulerTick(tick time.Duration) Option {
	return func(m *meeseek) {
		m.schedulerTick = tick
	}
}

func recordingProgram(name, runsFile string, opts ...program.Option) program.Program {
	args := program.Args("-c", recordRunAndSleep, "sh", runsFile)
	return program.New(name, "sh", append([]program.Option{args}, opts...)...)
}

func countRuns(t *testing.T, runsFile string) int {
	t.Helper()

	data, err := os.ReadFile(runsFile)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("read runs file: %v", err)
	}
	return strings.Count(string(data), "run\n")
}

func waitForRuns(t *testing.T, runsFile string, want int) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if countRuns(t, runsFile) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("program ran %d times, want at least %d", countRuns(t, runsFile), want)
}

func shutdownOnCleanup(t *testing.T, m Meeseek) {
	t.Helper()
	t.Cleanup(func() { _ = m.Shutdown(2 * time.Second) })
}

func TestMeeseekReload_RemovingRunningScheduledProgramKeepsManagerAlive(t *testing.T) {
	t.Parallel()

	for i := range reloadRaceInstances {
		t.Run(fmt.Sprintf("instance %d", i), func(t *testing.T) {
			t.Parallel()

			runs := filepath.Join(t.TempDir(), "runs")
			m := New(withSchedulerTick(testSchedulerTick))
			shutdownOnCleanup(t, m)
			removed := recordingProgram("removed", runs, program.Interval(testInterval))
			if err := m.AddProgram(removed); err != nil {
				t.Fatalf("AddProgram() error = %v", err)
			}

			ctx := t.Context()
			m.Start(ctx)
			waitForRuns(t, runs, 1)
			time.Sleep(schedulerTickWait)

			m.Reload(ctx, []program.Program{program.New("replacement", "true")}, time.Second)
			time.Sleep(50 * time.Millisecond)

			if _, err := m.Statistic("replacement"); err != nil {
				t.Fatalf("Statistic() after reload error = %v", err)
			}
		})
	}
}

func TestMeeseekReload_ChangedScheduledProgramKeepsItsSchedule(t *testing.T) {
	t.Parallel()

	for i := range reloadRaceInstances {
		t.Run(fmt.Sprintf("instance %d", i), func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			oldRuns := filepath.Join(dir, "old")
			newRuns := filepath.Join(dir, "new")
			m := New(withSchedulerTick(testSchedulerTick))
			shutdownOnCleanup(t, m)
			oldVersion := recordingProgram("job", oldRuns, program.Interval(testInterval))
			if err := m.AddProgram(oldVersion); err != nil {
				t.Fatalf("AddProgram() error = %v", err)
			}

			ctx := t.Context()
			m.Start(ctx)
			waitForRuns(t, oldRuns, 1)
			time.Sleep(schedulerTickWait)

			newVersion := program.New(
				"job",
				"sh",
				program.Args("-c", `echo run >> "$1"`, "sh", newRuns),
				program.Interval(testInterval),
			)
			m.Reload(ctx, []program.Program{newVersion}, time.Second)

			waitForRuns(t, newRuns, 2)
		})
	}
}

func TestMeeseek_StopEndsPendingRetries(t *testing.T) {
	t.Parallel()

	const failAfterRecording = `echo run >> "$1"; exit 1`

	tests := []struct {
		name    string
		script  string
		options []program.Option
		launch  func(context.Context, Meeseek) error
	}{
		{
			name:    "program launched by Start",
			script:  recordRunAndSleep,
			options: []program.Option{program.RetryCount(3)},
			launch: func(ctx context.Context, m Meeseek) error {
				m.Start(ctx)
				return nil
			},
		},
		{
			name:    "program launched by RunAsync",
			script:  recordRunAndSleep,
			options: []program.Option{program.RetryCount(3)},
			launch: func(_ context.Context, m Meeseek) error {
				return m.RunAsync("job")
			},
		},
		{
			name:   "program waiting for its retry delay",
			script: failAfterRecording,
			options: []program.Option{
				program.RetryCount(3),
				program.RetryDelay(400 * time.Millisecond),
			},
			launch: func(ctx context.Context, m Meeseek) error {
				m.Start(ctx)
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runs := filepath.Join(t.TempDir(), "runs")
			m := New()
			shutdownOnCleanup(t, m)
			args := program.Args("-c", tt.script, "sh", runs)
			opts := append([]program.Option{args}, tt.options...)
			if err := m.AddProgram(program.New("job", "sh", opts...)); err != nil {
				t.Fatalf("AddProgram() error = %v", err)
			}

			ctx := t.Context()
			if err := tt.launch(ctx, m); err != nil {
				t.Fatalf("launch error = %v", err)
			}
			waitForRuns(t, runs, 1)

			if err := m.Stop("job", 2*time.Second); err != nil {
				t.Fatalf("Stop() error = %v", err)
			}
			time.Sleep(800 * time.Millisecond)

			if got := countRuns(t, runs); got != 1 {
				t.Fatalf("program ran %d times after Stop, want 1", got)
			}
		})
	}
}

func TestMeeseek_ProgramInInitialDelayNeverRunsAfterBeingStopped(t *testing.T) {
	t.Parallel()

	const initialDelay = 400 * time.Millisecond

	tests := []struct {
		name string
		stop func(context.Context, Meeseek)
	}{
		{
			name: "Stop",
			stop: func(_ context.Context, m Meeseek) {
				if err := m.Stop("delayed", time.Second); err != nil {
					t.Errorf("Stop() error = %v", err)
				}
			},
		},
		{
			name: "Shutdown",
			stop: func(_ context.Context, m Meeseek) {
				if err := m.Shutdown(time.Second); err != nil {
					t.Errorf("Shutdown() error = %v", err)
				}
			},
		},
		{
			name: "Reload removing the program",
			stop: func(ctx context.Context, m Meeseek) {
				m.Reload(ctx, []program.Program{program.New("other", "true")}, time.Second)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runs := filepath.Join(t.TempDir(), "runs")
			m := New()
			shutdownOnCleanup(t, m)
			delayed := recordingProgram("delayed", runs, program.InitialDelay(initialDelay))
			if err := m.AddProgram(delayed); err != nil {
				t.Fatalf("AddProgram() error = %v", err)
			}

			ctx := t.Context()
			m.Start(ctx)
			tt.stop(ctx, m)
			time.Sleep(2 * initialDelay)

			if got := countRuns(t, runs); got != 0 {
				t.Fatalf("program ran %d times after being stopped, want 0", got)
			}
		})
	}
}

func TestMeeseek_StoppingDuringInitialDelayEndsTheLaunchWithoutFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		stop func(Meeseek) error
	}{
		{
			name: "Stop",
			stop: func(m Meeseek) error { return m.Stop("delayed", time.Second) },
		},
		{
			name: "Shutdown",
			stop: func(m Meeseek) error { return m.Shutdown(time.Second) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runs := filepath.Join(t.TempDir(), "runs")
			m := New()
			shutdownOnCleanup(t, m)
			delayed := recordingProgram("delayed", runs, program.InitialDelay(time.Hour))
			if err := m.AddProgram(delayed); err != nil {
				t.Fatalf("AddProgram() error = %v", err)
			}

			ctx := t.Context()
			m.Start(ctx)
			if err := tt.stop(m); err != nil {
				t.Fatalf("stop error = %v", err)
			}

			waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
			defer waitCancel()
			if err := m.Wait(waitCtx); err != nil {
				t.Fatalf("Wait() after stopping delayed program: %v", err)
			}

			stats, err := m.Statistic("delayed")
			if err != nil {
				t.Fatalf("Statistic() error = %v", err)
			}
			if stats.Failed != 0 {
				t.Fatalf("Failed = %d, want 0 for a program that never ran", stats.Failed)
			}
		})
	}
}
