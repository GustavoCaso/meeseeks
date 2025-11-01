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

		output := p.Stdout()
		if !strings.Contains(output, "hello world") {
			t.Fatalf("Expected output to contain 'hello world', got: %q", output)
		}

		if p.Stderr() != "" {
			t.Fatalf("Expected no error output, got: %q", p.Stderr())
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

		errorMessage := p.Stderr()
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

		if errOutput := p.Stderr(); !strings.Contains(errOutput, "error message") {
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

		if output := p.Stdout(); !strings.Contains(output, "hello from stdin") {
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

		if output := p.Stdout(); !strings.Contains(output, "test_value") {
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

		output := p.Stdout()
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

		errorMessage := p.Stderr()
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

		errorMessage := p.Stderr()
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

		output := p.Stdout()
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
					_ = p.Stdout()
					_ = p.Stderr()
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

		if len(p.Stdout()) == 0 {
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

		output := p.Stdout()
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

		output := p.Stdout()
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
			output := p.Stdout()
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

func TestFileOutput(t *testing.T) {
	t.Parallel()
	t.Run("stdout to file", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stdoutFile := filepath.Join(tmpDir, "stdout.log")

		p := New("stdout-file-test", "echo", Args("hello stdout file"), StdoutFile(stdoutFile))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		// Verify file was created and contains expected content
		content, err := os.ReadFile(stdoutFile)
		if err != nil {
			t.Fatalf("Failed to read stdout file: %v", err)
		}

		if !strings.Contains(string(content), "hello stdout file") {
			t.Fatalf("Expected stdout file to contain 'hello stdout file', got: %q", string(content))
		}

		// Verify program's internal Output() still works
		output := p.Stdout()
		if !strings.Contains(output, "hello stdout file") {
			t.Fatalf("Expected program output to contain 'hello stdout file', got: %q", output)
		}
	})

	t.Run("stderr to file", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stderrFile := filepath.Join(tmpDir, "stderr.log")

		p := New("stderr-file-test", "bash", Args("-c", "echo 'error message' >&2; exit 0"), StderrFile(stderrFile))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		// Verify file was created and contains expected content
		content, err := os.ReadFile(stderrFile)
		if err != nil {
			t.Fatalf("Failed to read stderr file: %v", err)
		}

		if !strings.Contains(string(content), "error message") {
			t.Fatalf("Expected stderr file to contain 'error message', got: %q", string(content))
		}

		// Verify program's internal Error() still works
		errorOutput := p.Stderr()
		if !strings.Contains(errorOutput, "error message") {
			t.Fatalf("Expected program error to contain 'error message', got: %q", errorOutput)
		}
	})

	t.Run("both stdout and stderr to files", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stdoutFile := filepath.Join(tmpDir, "stdout.log")
		stderrFile := filepath.Join(tmpDir, "stderr.log")

		p := New("dual-file-test", "bash",
			Args("-c", "echo 'stdout message'; echo 'stderr message' >&2"),
			StdoutFile(stdoutFile),
			StderrFile(stderrFile))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		// Verify stdout file
		stdoutContent, err := os.ReadFile(stdoutFile)
		if err != nil {
			t.Fatalf("Failed to read stdout file: %v", err)
		}
		if !strings.Contains(string(stdoutContent), "stdout message") {
			t.Fatalf("Expected stdout file to contain 'stdout message', got: %q", string(stdoutContent))
		}

		// Verify stderr file
		stderrContent, err := os.ReadFile(stderrFile)
		if err != nil {
			t.Fatalf("Failed to read stderr file: %v", err)
		}
		if !strings.Contains(string(stderrContent), "stderr message") {
			t.Fatalf("Expected stderr file to contain 'stderr message', got: %q", string(stderrContent))
		}
	})

	t.Run("file output with custom writers", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stdoutFile := filepath.Join(tmpDir, "stdout.log")
		var customBuf bytes.Buffer

		p := New("mixed-output-test", "echo", Args("mixed output test"),
			StdoutFile(stdoutFile),
			Stdout(&customBuf))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		// Verify file output
		fileContent, err := os.ReadFile(stdoutFile)
		if err != nil {
			t.Fatalf("Failed to read stdout file: %v", err)
		}
		if !strings.Contains(string(fileContent), "mixed output test") {
			t.Fatalf("Expected file to contain 'mixed output test', got: %q", string(fileContent))
		}

		// Verify custom writer output
		if !strings.Contains(customBuf.String(), "mixed output test") {
			t.Fatalf("Expected custom buffer to contain 'mixed output test', got: %q", customBuf.String())
		}

		// Verify program's internal output
		if !strings.Contains(p.Stdout(), "mixed output test") {
			t.Fatalf("Expected program output to contain 'mixed output test', got: %q", p.Stdout())
		}
	})

	t.Run("file output with directory creation", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		// Create nested directory structure
		stdoutFile := filepath.Join(tmpDir, "nested", "dir", "stdout.log")

		p := New("nested-dir-test", "echo", Args("directory creation test"), StdoutFile(stdoutFile))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		// Verify file was created in nested directories
		content, err := os.ReadFile(stdoutFile)
		if err != nil {
			t.Fatalf("Failed to read stdout file: %v", err)
		}

		if !strings.Contains(string(content), "directory creation test") {
			t.Fatalf("Expected stdout file to contain 'directory creation test', got: %q", string(content))
		}
	})

	t.Run("file output append mode", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stdoutFile := filepath.Join(tmpDir, "append.log")

		// Write initial content to file
		err := os.WriteFile(stdoutFile, []byte("initial content\n"), 0600)
		if err != nil {
			t.Fatalf("Failed to create initial file: %v", err)
		}

		p := New("append-test", "echo", Args("appended content"), StdoutFile(stdoutFile))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		// Verify file contains both initial and appended content
		content, err := os.ReadFile(stdoutFile)
		if err != nil {
			t.Fatalf("Failed to read stdout file: %v", err)
		}

		contentStr := string(content)
		if !strings.Contains(contentStr, "initial content") {
			t.Fatalf("Expected file to contain 'initial content', got: %q", contentStr)
		}
		if !strings.Contains(contentStr, "appended content") {
			t.Fatalf("Expected file to contain 'appended content', got: %q", contentStr)
		}
	})

	t.Run("file permissions after creation", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stdoutFile := filepath.Join(tmpDir, "permissions_stdout.log")

		p := New("permission-test", "echo", Args("testing permissions"), StdoutFile(stdoutFile))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		// Check file permissions
		info, err := os.Stat(stdoutFile)
		if err != nil {
			t.Fatalf("Failed to stat stdout file: %v", err)
		}

		// File should be created with 0600 permissions (owner read/write only)
		expectedPerm := os.FileMode(0600)
		if info.Mode().Perm() != expectedPerm {
			t.Fatalf("Expected file permissions %v, got %v", expectedPerm, info.Mode().Perm())
		}
	})
}

func TestFileOutputError(t *testing.T) {
	t.Parallel()
	t.Run("invalid stdout file path", func(t *testing.T) {
		t.Parallel()
		// Try to create file in non-existent root directory (should fail)
		invalidPath := "/nonexistent/root/dir/stdout.log"
		p := New("invalid-stdout-test", "echo", Args("test"), StdoutFile(invalidPath))

		_, err := p.Start(t.Context())
		if err == nil {
			t.Fatal("Expected error for invalid stdout file path, got nil")
		}

		if !strings.Contains(err.Error(), "failed to open stdout file") {
			t.Fatalf("Expected error message to mention stdout file, got: %q", err.Error())
		}
	})

	t.Run("invalid stderr file path", func(t *testing.T) {
		t.Parallel()
		// Try to create file in non-existent root directory (should fail)
		invalidPath := "/nonexistent/root/dir/stderr.log"
		p := New("invalid-stderr-test", "echo", Args("test"), StderrFile(invalidPath))

		_, err := p.Start(t.Context())
		if err == nil {
			t.Fatal("Expected error for invalid stderr file path, got nil")
		}

		if !strings.Contains(err.Error(), "failed to open stderr file") {
			t.Fatalf("Expected error message to mention stderr file, got: %q", err.Error())
		}
	})

	t.Run("permission denied file creation", func(t *testing.T) {
		t.Parallel()
		// Create a read-only directory
		tmpDir := t.TempDir()
		readOnlyDir := filepath.Join(tmpDir, "readonly")
		err := os.Mkdir(readOnlyDir, 0400) // Read-only directory
		if err != nil {
			t.Fatalf("Failed to create read-only directory: %v", err)
		}

		stdoutFile := filepath.Join(readOnlyDir, "stdout.log")
		p := New("permission-test", "echo", Args("test"), StdoutFile(stdoutFile))

		_, err = p.Start(t.Context())
		if err == nil {
			t.Fatal("Expected error for permission denied, got nil")
		}

		if !strings.Contains(err.Error(), "failed to open stdout file") {
			t.Fatalf("Expected error message to mention stdout file, got: %q", err.Error())
		}
	})
}

func TestFileOutputNotOutput(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stdoutFile := filepath.Join(tmpDir, "empty_stdout.log")

	// Program that produces no output
	p := New("empty-output-test", "true", StdoutFile(stdoutFile))

	done, err := p.Start(t.Context())
	if err != nil {
		t.Fatalf("Failed to start program: %v", err)
	}
	<-done

	// File should exist but be empty
	info, err := os.Stat(stdoutFile)
	if err != nil {
		t.Fatalf("Expected stdout file to exist: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("Expected empty file, got size %d", info.Size())
	}
}

func TestBufferSizeLimit(t *testing.T) {
	t.Parallel()

	t.Run("no limit - unlimited buffer growth", func(t *testing.T) {
		t.Parallel()
		p := New(
			"no-limit-test",
			"bash",
			Args("-c", "for i in {1..1000}; do echo \"line $i of unlimited output\"; done"),
		)

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		output := p.Stdout()

		if strings.Contains(output, "truncated due to buffer limit") {
			t.Fatalf("Expected output to not be truncated")
		}
	})

	t.Run("zero buffer limit disables limiting", func(t *testing.T) {
		t.Parallel()
		p := New("zero-limit-test", "bash",
			Args("-c", "for i in {1..100}; do echo \"line $i without limit\"; done"),
			BufferSizeLimit(0))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		output := p.Stdout()

		if strings.Contains(output, "truncated") {
			t.Fatalf("Expected no truncation with zero limit, but found truncation message")
		}
	})

	t.Run("buffer limit triggers truncation", func(t *testing.T) {
		t.Parallel()
		bufferLimit := 1000
		p := New("limit-test", "bash",
			Args("-c", "for i in {1..200}; do echo \"line $i with some padding text to make it longer\"; done"),
			BufferSizeLimit(bufferLimit))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		output := p.Stdout()

		if !strings.Contains(output, "truncated due to buffer limit") {
			t.Fatalf("Expected truncation message in output, got: %q", output)
		}

		// Output should not exceed significantly beyond threshold (95% of limit)
		threshold := int(float64(bufferLimit) * 0.95)
		if len(output) > bufferLimit+200 { // Allow some overhead for truncation message
			t.Fatalf(
				"Expected output to be limited, got %d bytes (threshold: %d, limit: %d)",
				len(output),
				threshold,
				bufferLimit,
			)
		}
	})

	t.Run("buffer limit applies to error output", func(t *testing.T) {
		t.Parallel()
		bufferLimit := 500
		p := New("error-limit-test", "bash",
			Args("-c", "for i in {1..100}; do echo \"error line $i with padding text\" >&2; done; exit 1"),
			BufferSizeLimit(bufferLimit))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		errorOutput := p.Stderr()

		if !strings.Contains(errorOutput, "truncated due to buffer limit") {
			t.Fatalf("Expected truncation message in error output, got: %q", errorOutput)
		}

		if len(errorOutput) > bufferLimit+200 {
			t.Fatalf("Expected error output to be limited, got %d bytes", len(errorOutput))
		}
	})

	t.Run("buffer limit with custom IO writers", func(t *testing.T) {
		t.Parallel()
		var customBuf bytes.Buffer
		bufferLimit := 300

		p := New("custom-io-limit-test", "bash",
			Args("-c", "for i in {1..50}; do echo \"custom output line $i\"; done"),
			BufferSizeLimit(bufferLimit),
			Stdout(&customBuf))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		internalOutput := p.Stdout()
		if !strings.Contains(internalOutput, "truncated") && len(internalOutput) > bufferLimit+200 {
			t.Fatalf("Expected internal buffer to be limited")
		}

		customOutput := customBuf.String()
		if !strings.Contains(customOutput, "line 50") {
			t.Fatalf("Expected custom writer to receive all output")
		}

		if len(customOutput) <= len(internalOutput) {
			t.Fatalf("Expected custom writer to have all output")
		}
	})
}

func TestProgram_SubscribeLogs(t *testing.T) {
	t.Parallel()

	t.Run("basic functionality", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		p := New("log-test", "bash",
			Args("-c", "echo 'line1'; echo 'line2'; echo 'line3'"),
			Async())

		logCh := p.SubscribeLogs(ctx)

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		var received []LogLine
		timeout := time.After(1 * time.Second)

	collectLoop:
		for {
			select {
			case log := <-logCh:
				received = append(received, log)
			case <-done:
				// Program finished, drain remaining logs
				for {
					select {
					case log := <-logCh:
						received = append(received, log)
					default:
						// No more logs to drain
						break collectLoop
					}
				}
			case <-timeout:
				t.Fatal("Timeout waiting for logs")
			}
		}

		if len(received) < 3 {
			t.Fatalf("Expected at least 3 log lines, got %d", len(received))
		}
	})

	t.Run("historical logs", func(t *testing.T) {
		t.Parallel()

		p := New("historical-test", "echo", Args("historical output"))

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}
		<-done

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		logCh := p.SubscribeLogs(ctx)

		timeout := time.After(1 * time.Second)
		var received []LogLine

	collectLoop:
		for {
			select {
			case log := <-logCh:
				received = append(received, log)
				if len(received) == 1 {
					break collectLoop
				}
			case <-timeout:
				break collectLoop
			}
		}

		if len(received) != 1 {
			t.Fatal("Expected to receive historical logs")
		}
	})

	t.Run("multiple subscribers", func(t *testing.T) {
		t.Parallel()

		p := New("multi-sub-test", "bash",
			Args("-c", "for i in {1..5}; do echo \"message $i\"; sleep 0.05; done"),
			Async())

		ctx1, cancel1 := context.WithCancel(t.Context())
		ctx2, cancel2 := context.WithCancel(t.Context())
		ctx3, cancel3 := context.WithCancel(t.Context())

		logCh1 := p.SubscribeLogs(ctx1)
		logCh2 := p.SubscribeLogs(ctx2)
		logCh3 := p.SubscribeLogs(ctx3)

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		allReceived := make([][]LogLine, 3)

		collectFunc := func(id int, ch <-chan LogLine) {
			defer wg.Done()
			timeout := time.After(3 * time.Second)
			for {
				select {
				case log, ok := <-ch:
					if !ok {
						return
					}
					mu.Lock()
					allReceived[id] = append(allReceived[id], log)
					mu.Unlock()
				case <-timeout:
					return
				}
			}
		}

		wg.Add(3)
		go collectFunc(0, logCh1)
		go collectFunc(1, logCh2)
		go collectFunc(2, logCh3)

		<-done

		cancel1()
		cancel2()
		cancel3()
		wg.Wait()

		for i, logs := range allReceived {
			if len(logs) != 5 {
				t.Errorf("Subscriber %d received %d logs, expected 5", i, len(logs))
			}
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()

		p := New("cancel-sub-test", "bash",
			Args("-c", "for i in {1..100}; do echo \"line $i\"; sleep 0.05; done"),
			Async())

		ctx, cancel := context.WithCancel(t.Context())
		logCh := p.SubscribeLogs(ctx)

		_, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		receivedCount := 0
		timeout := time.After(1 * time.Second)
		for receivedCount < 3 {
			select {
			case <-logCh:
				receivedCount++
			case <-timeout:
				t.Fatal("Timeout waiting for initial logs")
			}
		}

		cancel()

		// Channel should be closed
		_, ok := <-logCh
		if ok {
			t.Error("Expected channel to be closed after context cancellation")
		}

		shutdownErr := p.Shutdown(1 * time.Second)
		if shutdownErr != nil {
			t.Fatalf("Error shutting down the program %s", shutdownErr.Error())
		}
	})

	t.Run("program finishes continues streaming", func(t *testing.T) {
		t.Parallel()

		p := New("finish-stream-test", "echo", Args("finish test"), Async())

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		logCh := p.SubscribeLogs(ctx)

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		<-done

		_, ok := <-logCh
		if !ok {
			t.Error("Expected channel to remain open after program finishes (tail -f behavior)")
		}

		// Explicitly cancel context
		cancel()
	})

	t.Run("stdout and stderr distinction", func(t *testing.T) {
		t.Parallel()

		p := New("stdout-stderr-test", "bash",
			Args("-c", "echo 'stdout message'; echo 'stderr message' >&2; echo 'stdout message 2'"),
			Async())

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		logCh := p.SubscribeLogs(ctx)

		done, err := p.Start(t.Context())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		var sdoutLogs []LogLine
		var stderrLogs []LogLine
		timeout := time.After(1 * time.Second)

	collectLoop:
		for {
			select {
			case log := <-logCh:
				if log.IsError {
					stderrLogs = append(stderrLogs, log)
				} else {
					sdoutLogs = append(sdoutLogs, log)
				}
			case <-done:
				// Drain remaining logs
				for {
					select {
					case log := <-logCh:
						if log.IsError {
							stderrLogs = append(stderrLogs, log)
						} else {
							sdoutLogs = append(sdoutLogs, log)
						}
					default:
						break collectLoop
					}
				}
			case <-timeout:
				t.Fatal("Timeout collecting logs")
			}
		}

		if len(stderrLogs) != 1 {
			t.Fatal("Did not receive stderr message")
		}

		if !strings.Contains(stderrLogs[0].Message, "stderr") {
			t.Fatalf(
				"Did not receive correct stderr message. Expected \"stderr message\" got: %s",
				stderrLogs[0].Message,
			)
		}

		if len(sdoutLogs) != 2 {
			t.Fatal("Did not receive stdout messages")
		}

		for _, log := range sdoutLogs {
			if strings.Contains(log.Message, "stderr") {
				t.Fatal("Did not receive correct stdout message. Message conatins stderr")
			}
		}
	})
}

func TestProgram_String(t *testing.T) {
	t.Parallel()

	duration := 1 * time.Second

	p := New("test", "bash",
		Args("-c", "echo hello"),
		Interval(duration),
		InitialDelay(duration),
	)

	expected := "name: test, command: bash, arguments: (-c, echo hello), interval: 1s, initial delay: 1s"

	if p.String() != expected {
		t.Fatalf("String() did not returned expected output. expected: %q, got: %q", expected, p.String())
	}
}
