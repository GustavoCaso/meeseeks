package program

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOneShot(t *testing.T) {
	t.Run("basic command execution", func(t *testing.T) {
		p := New("echo-test", "echo", Args("hello world"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		output := p.Output()
		if !strings.Contains(output, "hello world") {
			t.Errorf("Expected output to contain 'hello world', got: %q", output)
		}

		if p.Error() != "" {
			t.Errorf("Expected no error output, got: %q", p.Error())
		}
	})

	t.Run("exit code handling", func(t *testing.T) {
		p := New("failure-test", "bash", Args("-c", "exit 2"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		errorMessage := p.Error()
		if !strings.Contains(errorMessage, "exit status 2") {
			t.Errorf("Expected errors to contain 'exit status 2', got: %q", errorMessage)
		}
	})

	t.Run("stderr output", func(t *testing.T) {
		p := New("stderr-test", "bash", Args("-c", "echo 'error message' >&2; exit 1"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if errOutput := p.Error(); !strings.Contains(errOutput, "error message") {
			t.Errorf("Expected stderr to contain 'error message', got: %q", errOutput)
		}
	})
}

func TestCustomIO(t *testing.T) {
	t.Run("custom stdout", func(t *testing.T) {
		var buf bytes.Buffer
		p := New("stdout-test", "echo", Args("hello custom stdout"), Stdout(&buf))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if !strings.Contains(buf.String(), "hello custom stdout") {
			t.Errorf("Custom stdout should contain 'hello custom stdout', got: %q", buf.String())
		}
	})

	t.Run("custom stderr", func(t *testing.T) {
		var buf bytes.Buffer
		p := New("stderr-test", "bash", Args("-c", "echo 'custom error' >&2; exit 0"), Stderr(&buf))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if !strings.Contains(buf.String(), "custom error") {
			t.Errorf("Custom stderr should contain 'custom error', got: %q", buf.String())
		}
	})

	t.Run("custom stdin", func(t *testing.T) {
		input := strings.NewReader("hello from stdin")
		p := New("stdin-test", "cat", Stdin(input))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if output := p.Output(); !strings.Contains(output, "hello from stdin") {
			t.Errorf("Expected output to contain stdin content, got: %q", output)
		}
	})

	t.Run("custom env vars", func(t *testing.T) {
		p := New("env-test", "bash", Args("-c", "echo $CUSTOM_VAR"), Envs("CUSTOM_VAR=test_value"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if output := p.Output(); !strings.Contains(output, "test_value") {
			t.Errorf("Expected output to contain env var value, got: %q", output)
		}
	})

	t.Run("file output", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "meeseeks_test_output.txt")

		outFile, err := os.Create(tmpFile)
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		defer outFile.Close()

		p := New("file-test", "echo", Args("write to file"), Stdout(outFile))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		outFile.Close()

		content, err := os.ReadFile(tmpFile)
		if err != nil {
			t.Fatalf("Failed to read temp file: %v", err)
		}

		if !strings.Contains(string(content), "write to file") {
			t.Errorf("File should contain 'write to file', got: %q", string(content))
		}
	})
}

func TestAsync(t *testing.T) {
	t.Run("background process", func(t *testing.T) {
		p := New("long-test", "bash", Args("-c", "for i in {1..3}; do echo \"iteration $i\"; sleep 0.1; done"), Async())

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		select {
		case <-done:
			// Process completed
		case <-time.After(5 * time.Second):
			t.Fatal("Process failed to complete within timeout")
		}

		output := p.Output()
		if !strings.Contains(output, "iteration 1") {
			t.Errorf("Expected output to contain iteration 1, got: %q", output)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		p := New("cancel-test", "bash", Args("-c", "for i in {1..10}; do echo \"loop $i\"; sleep 0.5; done"), Async())

		done, err := p.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		cancel()

		select {
		case <-done:
			// Process completed (either finished or error)
		case <-time.After(5 * time.Second):
			t.Fatal("Process was not in the valid state")
		}

		errorMessage := p.Error()
		// Context cancellation can result in either error or finished status depending on timing
		if !strings.Contains(errorMessage, "signal: killed") {
			t.Errorf("Expected errorMessage to indicate error or finished after cancellation, got: %q", errorMessage)
		}
	})

	t.Run("custom IO with long running", func(t *testing.T) {
		var stdoutBuf, stderrBuf bytes.Buffer

		p := New("io-test", "bash",
			Args("-c", "echo 'stdout message'; echo 'stderr message' >&2; sleep 0.1; echo 'delayed message'"),
			Stdout(&stdoutBuf),
			Stderr(&stderrBuf),
			Async())

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		select {
		case <-done:
			// Process completed
		case <-time.After(5 * time.Second):
			t.Fatal("Process failed to complete within timeout")
		}

		if !strings.Contains(stdoutBuf.String(), "stdout message") ||
			!strings.Contains(stdoutBuf.String(), "delayed message") {
			t.Errorf("Expected custom stdout to contain messages, got: %q", stdoutBuf.String())
		}

		if !strings.Contains(stderrBuf.String(), "stderr message") {
			t.Errorf("Expected custom stderr to contain message, got: %q", stderrBuf.String())
		}
	})

	t.Run("lastLine tracking", func(t *testing.T) {
		p := New(
			"lastline-test",
			"bash",
			Args("-c", "echo 'line 1'; sleep 0.1; echo 'line 2'; sleep 0.1; echo 'final line'"),
			Async(),
		)

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		select {
		case <-done:
			// Process completed
		case <-time.After(5 * time.Second):
			t.Fatal("Process failed to complete within timeout")
		}

		lastLine := p.LastLine()
		if lastLine != "final line" {
			t.Errorf("Expected lastLine to be 'final line', got: %q", lastLine)
		}
	})
}

func TestEdgeCases(t *testing.T) {
	t.Run("command not found", func(t *testing.T) {
		p := New("not-exists", "command_that_does_not_exist")

		_, err := p.Start(t.Context())
		if err == nil {
			t.Fatal("Expected error for non-existent command but got nil")
		}

		errorMessage := p.Error()
		if !strings.Contains(errorMessage, "executable file not found in $PATH") {
			t.Errorf("Expected errorMessage to indicate error, got: %q", errorMessage)
		}
	})

	t.Run("empty command", func(t *testing.T) {
		p := New("empty", "")

		_, err := p.Start(t.Context())
		if err == nil {
			t.Fatal("Expected error for empty command but got nil")
		}
	})

	t.Run("large output handling", func(t *testing.T) {
		// Generate ~100KB of output
		p := New("large-output", "bash", Args("-c", "for i in {1..5000}; do echo \"line $i of large output\"; done"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		output := p.Output()
		if len(output) < 90000 {
			t.Errorf("Expected large output, got only %d bytes", len(output))
		}

		if !strings.Contains(output, "line 5000") {
			t.Errorf("Output should contain final line")
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		p := New(
			"concurrent-test",
			"bash",
			Args("-c", "for i in {1..20}; do echo \"output $i\"; sleep 0.05; done"),
			Async(),
		)

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		var wg sync.WaitGroup
		for range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 5 {
					_ = p.Output()
					_ = p.Error()
					_ = p.LastLine()
					time.Sleep(30 * time.Millisecond)
				}
			}()
		}

		wg.Wait()

		select {
		case <-done:
			// Process completed
		case <-time.After(5 * time.Second):
			t.Fatal("Process failed to complete within timeout")
		}

		if len(p.Output()) == 0 {
			t.Error("Expected some output from concurrent access test")
		}
	})
}

func TestSend(t *testing.T) {
	t.Run("send data to interactive command", func(t *testing.T) {
		p := New("cat-test", "cat", KeepStdinOpen(), Async())

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		// Send first line
		err = p.Send([]byte("hello world\n"))
		if err != nil {
			t.Fatalf("Failed to send data: %v", err)
		}

		// Wait a bit for output
		time.Sleep(100 * time.Millisecond)

		// Send second line
		err = p.Send([]byte("second line\n"))
		if err != nil {
			t.Fatalf("Failed to send second data: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		// Close stdin to finish cat
		err = p.CloseStdin()
		if err != nil {
			t.Fatalf("Failed to close stdin: %v", err)
		}

		select {
		case <-done:
			// Process completed
		case <-time.After(5 * time.Second):
			t.Fatal("Process failed to complete within timeout")
		}

		output := p.Output()
		if !strings.Contains(output, "hello world") {
			t.Errorf("Expected output to contain 'hello world', got: %q", output)
		}
		if !strings.Contains(output, "second line") {
			t.Errorf("Expected output to contain 'second line', got: %q", output)
		}
	})

	t.Run("send with custom stdin and runtime input", func(t *testing.T) {
		initialInput := strings.NewReader("initial input\n")
		p := New("cat-combined", "cat", Stdin(initialInput), KeepStdinOpen(), Async())

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		// Send additional data via Send()
		err = p.Send([]byte("runtime input\n"))
		if err != nil {
			t.Fatalf("Failed to send data: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		err = p.CloseStdin()
		if err != nil {
			t.Fatalf("Failed to close stdin: %v", err)
		}

		select {
		case <-done:
			// Process completed
		case <-time.After(5 * time.Second):
			t.Fatal("Process failed to complete within timeout")
		}

		output := p.Output()
		if !strings.Contains(output, "initial input") {
			t.Errorf("Expected output to contain 'initial input', got: %q", output)
		}
		if !strings.Contains(output, "runtime input") {
			t.Errorf("Expected output to contain 'runtime input', got: %q", output)
		}
	})

	t.Run("send to finished process should error", func(t *testing.T) {
		p := New("echo-finished", "echo", Args("done"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		// Process should be finished now
		if p.State() != StateFinished {
			t.Fatal("Expected process to be finished")
		}

		// Sending to finished process should error
		err = p.Send([]byte("too late\n"))
		if err == nil {
			t.Fatal("Expected error when sending to finished process")
		}
	})
}

func TestMultiplePrograms(t *testing.T) {
	t.Run("run multiple programs", func(t *testing.T) {
		var programs []Program
		for i := range 5 {
			p := New(
				fmt.Sprintf("prog-%d", i),
				"echo",
				Args(fmt.Sprintf("output from program %d", i)),
			)
			programs = append(programs, p)
		}

		for _, p := range programs {
			done, err := p.Start(t.Context())
			if err != nil {
				t.Fatalf("Failed to start program %s: %v", p.Name(), err)
			}
			<-done
		}

		for i, p := range programs {
			output := p.Output()
			expected := fmt.Sprintf("output from program %d", i)
			if !strings.Contains(output, expected) {
				t.Errorf("Program %s: expected output to contain %q, got: %q", p.Name(), expected, output)
			}
		}
	})
}

func TestInterval(t *testing.T) {
	t.Run("program with interval", func(t *testing.T) {
		p := New("echo-test", "echo", Args("hello world"), Interval(time.Duration(10)*time.Millisecond))

		ctx, cancel := context.WithCancel(t.Context())

		_, err := p.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start program %s: %v", p.Name(), err)
		}
		time.Sleep(1 * time.Second)
		cancel()

		if p.Runs() <= 0 {
			t.Fatalf("Failed to run program %s multiple times expected more than zero got zero", p.Name())
		}
	})

	t.Run("async program with interval", func(t *testing.T) {
		p := New("echo-test", "echo", Args("hello world"), Async(), Interval(time.Duration(10)*time.Millisecond))

		ctx, cancel := context.WithCancel(t.Context())

		_, err := p.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start program %s: %v", p.Name(), err)
		}
		time.Sleep(1 * time.Second)
		cancel()

		if p.Runs() <= 0 {
			t.Fatalf("Failed to run program %s multiple times expected more than zero got zero", p.Name())
		}
	})

	t.Run("interval program with error signals done", func(t *testing.T) {
		// Use a command that will fail to test error handling
		p := New("failing-interval", "command_that_does_not_exist", Interval(time.Duration(50)*time.Millisecond))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		done, err := p.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start interval program: %v", err)
		}

		// The done channel should be signaled when the command fails
		select {
		case <-done:
			// Expected: done channel should be signaled due to command error
		case <-time.After(2 * time.Second):
			t.Fatal("Expected done channel to be signaled when interval command fails, but it wasn't")
		}

		// Verify that the program is in error state
		if p.State() != StateError {
			t.Errorf("Expected program state to be StateError, got: %v", p.State())
		}

		// Verify error message is captured
		errorOutput := p.Error()
		if errorOutput == "" {
			t.Error("Expected error output to be captured, but it was empty")
		}
	})
}

func TestStatistics(t *testing.T) {
	t.Run("statistics for interval program", func(t *testing.T) {
		p := New("echo-test", "echo", Args("hello world"), Interval(time.Duration(10)*time.Millisecond))

		ctx, cancel := context.WithCancel(t.Context())

		_, err := p.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start program %s: %v", p.Name(), err)
		}
		time.Sleep(100 * time.Millisecond)
		cancel()

		stats := p.Statistics()

		if stats.ProgramName != "echo-test" {
			t.Errorf("Expected program name 'echo-test', got %q", stats.ProgramName)
		}

		if stats.TotalRuns <= 0 {
			t.Errorf("Expected at least 1 run, got %d", stats.TotalRuns)
		}

		if stats.Successful <= 0 {
			t.Errorf("Expected at least 1 successful run, got %d", stats.Successful)
		}

		if !stats.HasInterval {
			t.Error("Expected HasInterval to be true")
		}

		if stats.Interval != 10*time.Millisecond {
			t.Errorf("Expected interval 10ms, got %v", stats.Interval)
		}

		if stats.TotalOutputLines <= 0 {
			t.Errorf("Expected at least 1 output line, got %d", stats.TotalOutputLines)
		}

		stringOutput := stats.String()
		if !strings.Contains(stringOutput, "echo-test") {
			t.Errorf("Expected string output to contain program name, got: %q", stringOutput)
		}

		if !strings.Contains(stringOutput, "interval: 10ms") {
			t.Errorf("Expected string output to contain interval info, got: %q", stringOutput)
		}

		if stats.LastOutput == "" {
			t.Error("Expected LastOutput to be set")
		}

		if !strings.Contains(stringOutput, "last output:") {
			t.Errorf("Expected string output to contain last output info, got: %q", stringOutput)
		}
	})

	t.Run("statistics for failed program", func(t *testing.T) {
		p := New("failure-test", "bash", Args("-c", "echo 'before error'; exit 1"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		stats := p.Statistics()

		if stats.TotalRuns != 1 {
			t.Errorf("Expected 1 run, got %d", stats.TotalRuns)
		}

		if stats.Failed != 1 {
			t.Errorf("Expected 1 failed run, got %d", stats.Failed)
		}

		if stats.Successful != 0 {
			t.Errorf("Expected 0 successful runs, got %d", stats.Successful)
		}

		if stats.LastError == "" {
			t.Error("Expected LastError to be set")
		}

		stringOutput := stats.String()
		if !strings.Contains(stringOutput, "failed: 1") {
			t.Errorf("Expected string output to show failed runs, got: %q", stringOutput)
		}

		if stats.LastOutput == "" {
			t.Error("Expected LastOutput to be set even for failed programs")
		}

		if !strings.Contains(stats.LastOutput, "before error") {
			t.Errorf("Expected LastOutput to contain output before error, got: %q", stats.LastOutput)
		}
	})

	t.Run("statistics for program with no runs", func(t *testing.T) {
		p := New("no-run-test", "echo", Args("hello"))

		stats := p.Statistics()

		if stats.TotalRuns != 0 {
			t.Errorf("Expected 0 runs, got %d", stats.TotalRuns)
		}

		stringOutput := stats.String()
		if !strings.Contains(stringOutput, "No runs completed yet") {
			t.Errorf("Expected message about no runs, got: %q", stringOutput)
		}
	})
}

func TestShutdown(t *testing.T) {
	t.Run("graceful shutdown of running process", func(t *testing.T) {
		p := New("sleep-test", "sleep", Args("10"), Async())

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		// Give process time to start
		time.Sleep(100 * time.Millisecond)

		if p.State() != StateRunning {
			t.Fatalf("Expected process to be running, got: %v", p.State())
		}

		// Test graceful shutdown
		start := time.Now()
		err = p.Shutdown(2 * time.Second)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Shutdown failed: %v", err)
		}

		// Should terminate quickly (much less than timeout)
		if duration > 1*time.Second {
			t.Errorf("Expected quick termination, took %v", duration)
		}

		// Wait for done channel
		select {
		case <-done:
			// Expected
		case <-time.After(1 * time.Second):
			t.Error("Process should have signaled done after graceful shutdown")
		}
	})

	t.Run("graceful shutdown with timeout fallback", func(t *testing.T) {
		// Create a process that ignores SIGTERM
		p := New("ignore-sigterm", "bash", Args("-c",
			"trap '' TERM; echo 'started'; while true; do sleep 0.1; done"), Async())

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		// Wait for process to start and install signal handler
		time.Sleep(200 * time.Millisecond)

		if p.State() != StateRunning {
			t.Fatalf("Expected process to be running, got: %v", p.State())
		}

		// Test graceful shutdown with short timeout - should fall back to force kill
		start := time.Now()
		err = p.Shutdown(300 * time.Millisecond)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Shutdown failed: %v", err)
		}

		// Should timeout and force kill (duration should be close to timeout)
		if duration < 250*time.Millisecond || duration > 500*time.Millisecond {
			t.Errorf("Expected timeout around 300ms, took %v", duration)
		}

		// Wait for done channel
		select {
		case <-done:
			// Expected
		case <-time.After(1 * time.Second):
			t.Error("Process should have signaled done after force kill")
		}
	})

	t.Run("graceful shutdown of already finished process", func(t *testing.T) {
		p := New("echo-test", "echo", Args("hello"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if p.State() != StateFinished {
			t.Fatalf("Expected process to be finished, got: %v", p.State())
		}

		// Graceful shutdown of finished process should be no-op
		err = p.Shutdown(1 * time.Second)
		if err != nil {
			t.Errorf("Shutdown of finished process should not error: %v", err)
		}
	})
}

func TestIntervalShutdown(t *testing.T) {
	t.Run("graceful shutdown stops interval loop", func(t *testing.T) {
		p := New("echo-interval", "echo", Args("tick"), Interval(100*time.Millisecond))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		done, err := p.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start interval program: %v", err)
		}

		// Let it run a few iterations
		time.Sleep(350 * time.Millisecond)
		initialRuns := p.Runs()

		if initialRuns < 2 {
			t.Fatalf("Expected at least 2 runs, got %d", initialRuns)
		}

		// Graceful shutdown should stop the interval
		err = p.Shutdown(1 * time.Second)
		if err != nil {
			t.Errorf("Shutdown failed: %v", err)
		}

		// Wait for done signal
		select {
		case <-done:
			// Expected
		case <-time.After(2 * time.Second):
			t.Error("Interval program should have signaled done after graceful shutdown")
		}

		// No more runs should happen
		finalRuns := p.Runs()
		time.Sleep(300 * time.Millisecond)
		afterWaitRuns := p.Runs()

		if afterWaitRuns != finalRuns {
			t.Errorf("Expected no more runs after shutdown, but runs increased from %d to %d",
				finalRuns, afterWaitRuns)
		}
	})

	t.Run("interval with long-running async command graceful shutdown", func(t *testing.T) {
		// Interval program where each iteration is a long-running async command
		// Use a shorter interval so the first run starts quickly
		p := New("sleep-interval", "sleep", Args("1"), Async(), Interval(200*time.Millisecond))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		done, err := p.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start interval program: %v", err)
		}

		// Wait for first interval tick + time for command to start
		time.Sleep(400 * time.Millisecond)

		// Check if a process is running (which means it started)
		if p.State() != StateRunning {
			t.Fatalf("Expected current iteration to be running, got: %v (runs: %d)", p.State(), p.Runs())
		}

		// Graceful shutdown should stop interval and current process
		start := time.Now()
		err = p.Shutdown(500 * time.Millisecond)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Shutdown failed: %v", err)
		}

		// Should terminate current sleep process quickly
		if duration > 400*time.Millisecond {
			t.Errorf("Expected quick termination, took %v", duration)
		}

		select {
		case <-done:
			// Expected
		case <-time.After(1 * time.Second):
			t.Error("Interval program should have signaled done")
		}

		// Should not start new iterations
		finalRuns := p.Runs()
		time.Sleep(500 * time.Millisecond) // Wait longer than interval
		afterWaitRuns := p.Runs()

		if afterWaitRuns != finalRuns {
			t.Errorf("Expected no new iterations after shutdown, but runs increased from %d to %d",
				finalRuns, afterWaitRuns)
		}
	})
}
