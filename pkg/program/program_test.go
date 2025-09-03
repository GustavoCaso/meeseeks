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
	t.Parallel()
	t.Run("basic command execution", func(t *testing.T) {
		t.Parallel()
		p := New("echo-test", "echo", Args("hello world"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		output := p.Output()
		if !strings.Contains(output, "hello world") {
			t.Fatalf("Expected output to contain 'hello world', got: %q", output)
		}

		if p.Error() != "" {
			t.Fatalf("Expected no error output, got: %q", p.Error())
		}
	})

	t.Run("exit code handling", func(t *testing.T) {
		t.Parallel()
		p := New("failure-test", "bash", Args("-c", "exit 2"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		errorMessage := p.Error()
		if !strings.Contains(errorMessage, "exit status 2") {
			t.Fatalf("Expected errors to contain 'exit status 2', got: %q", errorMessage)
		}
	})

	t.Run("stderr output", func(t *testing.T) {
		t.Parallel()
		p := New("stderr-test", "bash", Args("-c", "echo 'error message' >&2; exit 1"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if errOutput := p.Error(); !strings.Contains(errOutput, "error message") {
			t.Fatalf("Expected stderr to contain 'error message', got: %q", errOutput)
		}
	})
}

func TestCustomIO(t *testing.T) {
	t.Parallel()
	t.Run("custom stdout", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		p := New("stdout-test", "echo", Args("hello custom stdout"), Stdout(&buf))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if !strings.Contains(buf.String(), "hello custom stdout") {
			t.Fatalf("Custom stdout should contain 'hello custom stdout', got: %q", buf.String())
		}
	})

	t.Run("custom stderr", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		p := New("stderr-test", "bash", Args("-c", "echo 'custom error' >&2; exit 0"), Stderr(&buf))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if !strings.Contains(buf.String(), "custom error") {
			t.Fatalf("Custom stderr should contain 'custom error', got: %q", buf.String())
		}
	})

	t.Run("custom stdin", func(t *testing.T) {
		t.Parallel()
		input := strings.NewReader("hello from stdin")
		p := New("stdin-test", "cat", Stdin(input))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if output := p.Output(); !strings.Contains(output, "hello from stdin") {
			t.Fatalf("Expected output to contain stdin content, got: %q", output)
		}
	})

	t.Run("custom env vars", func(t *testing.T) {
		t.Parallel()
		p := New("env-test", "bash", Args("-c", "echo $CUSTOM_VAR"), Envs("CUSTOM_VAR=test_value"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if output := p.Output(); !strings.Contains(output, "test_value") {
			t.Fatalf("Expected output to contain env var value, got: %q", output)
		}
	})

	t.Run("file output", func(t *testing.T) {
		t.Parallel()
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
			t.Fatalf("File should contain 'write to file', got: %q", string(content))
		}
	})
}

func TestAsync(t *testing.T) {
	t.Parallel()
	t.Run("background process", func(t *testing.T) {
		t.Parallel()
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
			t.Fatalf("Expected output to contain iteration 1, got: %q", output)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()
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

		state := StateToString[p.State()]

		if state != "cancelled" {
			t.Fatalf("cancelled programs should have 'cancelled' state, got: %s", state)
		}

		errorMessage := p.Error()
		// Context cancellation can result in either error or finished status depending on timing
		if !strings.Contains(errorMessage, "signal: killed") {
			t.Fatalf("Expected errorMessage to indicate error or finished after cancellation, got: %q", errorMessage)
		}
	})

	t.Run("custom IO with long running", func(t *testing.T) {
		t.Parallel()
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
			t.Fatalf("Expected custom stdout to contain messages, got: %q", stdoutBuf.String())
		}

		if !strings.Contains(stderrBuf.String(), "stderr message") {
			t.Fatalf("Expected custom stderr to contain message, got: %q", stderrBuf.String())
		}
	})

	t.Run("lastLine tracking", func(t *testing.T) {
		t.Parallel()
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
			t.Fatalf("Expected lastLine to be 'final line', got: %q", lastLine)
		}
	})
}

func TestEdgeCases(t *testing.T) {
	t.Parallel()
	t.Run("command not found", func(t *testing.T) {
		t.Parallel()
		p := New("not-exists", "command_that_does_not_exist")

		_, err := p.Start(t.Context())
		if err == nil {
			t.Fatal("Expected error for non-existent command but got nil")
		}

		errorMessage := p.Error()
		if !strings.Contains(errorMessage, "executable file not found in $PATH") {
			t.Fatalf("Expected errorMessage to indicate error, got: %q", errorMessage)
		}
	})

	t.Run("empty command", func(t *testing.T) {
		t.Parallel()
		p := New("empty", "")

		_, err := p.Start(t.Context())
		if err == nil {
			t.Fatal("Expected error for empty command but got nil")
		}
	})

	t.Run("large output handling", func(t *testing.T) {
		t.Parallel()
		// Generate ~100KB of output
		p := New("large-output", "bash", Args("-c", "for i in {1..5000}; do echo \"line $i of large output\"; done"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		output := p.Output()
		if len(output) < 90000 {
			t.Fatalf("Expected large output, got only %d bytes", len(output))
		}

		if !strings.Contains(output, "line 5000") {
			t.Fatalf("Output should contain final line")
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		t.Parallel()
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
			t.Fatal("Expected some output from concurrent access test")
		}
	})
}

func TestSend(t *testing.T) {
	t.Parallel()
	t.Run("send data to interactive command", func(t *testing.T) {
		t.Parallel()
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

		// Send second line
		err = p.Send([]byte("second line\n"))
		if err != nil {
			t.Fatalf("Failed to send second data: %v", err)
		}

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
			t.Fatalf("Expected output to contain 'hello world', got: %q", output)
		}
		if !strings.Contains(output, "second line") {
			t.Fatalf("Expected output to contain 'second line', got: %q", output)
		}
	})

	t.Run("send with custom stdin and runtime input", func(t *testing.T) {
		t.Parallel()
		initialInput := strings.NewReader("initial input\n")
		p := New("cat-combined", "cat", Stdin(initialInput), KeepStdinOpen(), Async())

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		// Send additional data via Send()
		err = p.Send([]byte("runtime input\n"))
		if err != nil {
			t.Fatalf("Failed to send data: %v", err)
		}

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
			t.Fatalf("Expected output to contain 'initial input', got: %q", output)
		}
		if !strings.Contains(output, "runtime input") {
			t.Fatalf("Expected output to contain 'runtime input', got: %q", output)
		}
	})

	t.Run("send to finished process should error", func(t *testing.T) {
		t.Parallel()
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
	t.Parallel()
	t.Run("run multiple programs", func(t *testing.T) {
		t.Parallel()
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
				t.Fatalf("Program %s: expected output to contain %q, got: %q", p.Name(), expected, output)
			}
		}
	})
}

func TestProgramState(t *testing.T) {
	t.Parallel()
	t.Run("program tracks state correctly", func(t *testing.T) {
		t.Parallel()
		p := New("echo-test", "echo", Args("hello world"))

		if p.State() != StateNotStarted {
			t.Fatalf("Expected initial state to be StateNotStarted, got %v", p.State())
		}

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program %s: %v", p.Name(), err)
		}
		<-done

		if p.State() != StateFinished {
			t.Fatalf("Expected final state to be StateFinished, got %v", p.State())
		}
	})

	t.Run("program tracks error state correctly", func(t *testing.T) {
		t.Parallel()
		p := New("failure-test", "bash", Args("-c", "echo 'before error'; exit 1"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		if p.State() != StateError {
			t.Fatalf("Expected final state to be StateError, got %v", p.State())
		}
	})
}

func TestShutdown(t *testing.T) {
	t.Parallel()
	t.Run("graceful shutdown of running process", func(t *testing.T) {
		t.Parallel()
		p := New("sleep-test", "sleep", Args("10"), Async())

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		// Test graceful shutdown
		start := time.Now()
		err = p.Shutdown(2 * time.Second)
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("Shutdown failed: %v", err)
		}

		// Should terminate quickly (much less than timeout)
		if duration > 2*time.Second {
			t.Fatalf("Expected quick termination, took %v", duration)
		}

		// Wait for done channel
		select {
		case <-done:
			// Expected
		case <-time.After(3 * time.Second):
			t.Fatal("Process should have signaled done after graceful shutdown")
		}
	})

	t.Run("graceful shutdown with timeout fallback", func(t *testing.T) {
		t.Parallel()
		// Create a process that ignores SIGTERM
		p := New("ignore-sigterm", "bash", Args("-c",
			"trap '' TERM; echo 'started'; while true; do sleep 0.1; done"), Async())

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		// Test graceful shutdown with short timeout - should fall back to force kill
		start := time.Now()
		err = p.Shutdown(300 * time.Millisecond)
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("Shutdown failed: %v", err)
		}

		// Should timeout and force kill (duration should be close to timeout)
		if duration > 400*time.Millisecond {
			t.Fatalf("Expected timeout around 300ms, took %v", duration)
		}

		// Wait for done channel
		select {
		case <-done:
			// Expected
		case <-time.After(1 * time.Second):
			t.Fatal("Process should have signaled done after force kill")
		}
	})

	t.Run("graceful shutdown of already finished process", func(t *testing.T) {
		t.Parallel()
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
			t.Fatalf("Shutdown of finished process should not error: %v", err)
		}
	})
}
