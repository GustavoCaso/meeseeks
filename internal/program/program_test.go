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

func waitForState(p Program, targetState ProcessState, timeout time.Duration) bool {
	done := make(chan bool, 1)

	go func() {
		for {
			if p.State() == targetState {
				done <- true
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func TestOneShot(t *testing.T) {
	t.Run("basic command execution", func(t *testing.T) {
		p := New("echo-test", "echo", Args("hello world"))

		err := p.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		output := p.Output()
		if !strings.Contains(output, "hello world") {
			t.Errorf("Expected output to contain 'hello world', got: %q", output)
		}

		if p.Error() != "" {
			t.Errorf("Expected no error output, got: %q", p.Error())
		}

		if status := p.Status(); !strings.Contains(status, "finished with exit code: 0") {
			t.Errorf("Expected status to indicate successful completion, got: %q", status)
		}
	})

	t.Run("exit code handling", func(t *testing.T) {
		p := New("failure-test", "bash", Args("-c", "exit 2"))

		err := p.Start(context.Background())
		if err == nil {
			t.Fatal("Expected error for non-zero exit code but got nil")
		}

		status := p.Status()
		if !strings.Contains(status, "error code: 2") {
			t.Errorf("Expected status to contain 'error code: 2', got: %q", status)
		}
	})

	t.Run("stderr output", func(t *testing.T) {
		p := New("stderr-test", "bash", Args("-c", "echo 'error message' >&2; exit 1"))

		err := p.Start(context.Background())
		if err == nil {
			t.Fatal("Expected error but got nil")
		}

		if errOutput := p.Error(); !strings.Contains(errOutput, "error message") {
			t.Errorf("Expected stderr to contain 'error message', got: %q", errOutput)
		}
	})
}

func TestCustomIO(t *testing.T) {
	t.Run("custom stdout", func(t *testing.T) {
		var buf bytes.Buffer
		p := New("stdout-test", "echo", Args("hello custom stdout"), Stdout(&buf))

		err := p.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		if !strings.Contains(buf.String(), "hello custom stdout") {
			t.Errorf("Custom stdout should contain 'hello custom stdout', got: %q", buf.String())
		}
	})

	t.Run("custom stderr", func(t *testing.T) {
		var buf bytes.Buffer
		p := New("stderr-test", "bash", Args("-c", "echo 'custom error' >&2; exit 0"), Stderr(&buf))

		err := p.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		if !strings.Contains(buf.String(), "custom error") {
			t.Errorf("Custom stderr should contain 'custom error', got: %q", buf.String())
		}
	})

	t.Run("custom stdin", func(t *testing.T) {
		input := strings.NewReader("hello from stdin")
		p := New("stdin-test", "cat", Stdin(input))

		err := p.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		if output := p.Output(); !strings.Contains(output, "hello from stdin") {
			t.Errorf("Expected output to contain stdin content, got: %q", output)
		}
	})

	t.Run("custom env vars", func(t *testing.T) {
		p := New("env-test", "bash", Args("-c", "echo $CUSTOM_VAR"), Envs("CUSTOM_VAR=test_value"))

		err := p.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		if output := p.Output(); !strings.Contains(output, "test_value") {
			t.Errorf("Expected output to contain env var value, got: %q", output)
		}
	})

	t.Run("file output", func(t *testing.T) {
		tmpFile := filepath.Join(os.TempDir(), "meeseeks_test_output.txt")
		defer os.Remove(tmpFile)

		outFile, err := os.Create(tmpFile)
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		defer outFile.Close()

		p := New("file-test", "echo", Args("write to file"), Stdout(outFile))

		err = p.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

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

func TestLongRunning(t *testing.T) {
	t.Run("background process", func(t *testing.T) {
		p := LongRunning("long-test", "bash", Args("-c", "for i in {1..3}; do echo \"iteration $i\"; sleep 0.1; done"))

		var startErr error
		go func() {
			startErr = p.Start(context.Background())
		}()

		if !waitForState(p, StateRunning, 5*time.Second) {
			t.Fatal("Process failed to start within timeout")
		}

		if startErr != nil {
			t.Fatalf("Failed to start program: %v", startErr)
		}

		if !waitForState(p, StateFinished, 5*time.Second) {
			t.Fatal("Process failed to complete within timeout")
		}

		output := p.Output()
		if !strings.Contains(output, "iteration 1") {
			t.Errorf("Expected output to contain iteration 1, got: %q", output)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p := LongRunning("cancel-test", "bash", Args("-c", "for i in {1..10}; do echo \"loop $i\"; sleep 0.5; done"))

		var startErr error
		go func() {
			startErr = p.Start(ctx)
		}()

		if !waitForState(p, StateRunning, 5*time.Second) {
			t.Fatal("Process failed to start within timeout")
		}

		if startErr != nil {
			t.Fatalf("Failed to start program: %v", startErr)
		}

		cancel()

		if !waitForState(p, StateError, 5*time.Second) {
			t.Fatal("Process was not in the valid state")
		}

		status := p.Status()
		// Context cancellation can result in either error or finished status depending on timing
		if !strings.Contains(status, "error") && !strings.Contains(status, "finished") {
			t.Errorf("Expected status to indicate error or finished after cancellation, got: %q", status)
		}
	})

	t.Run("custom IO with long running", func(t *testing.T) {
		var stdoutBuf, stderrBuf bytes.Buffer

		p := LongRunning("io-test", "bash",
			Args("-c", "echo 'stdout message'; echo 'stderr message' >&2; sleep 0.1; echo 'delayed message'"),
			Stdout(&stdoutBuf),
			Stderr(&stderrBuf))

		var startErr error
		go func() {
			startErr = p.Start(context.Background())
		}()

		if !waitForState(p, StateRunning, 5*time.Second) {
			t.Fatal("Process failed to start within timeout")
		}

		if startErr != nil {
			t.Fatalf("Failed to start program: %v", startErr)
		}

		if !waitForState(p, StateFinished, 5*time.Second) {
			t.Fatal("Process failed to complete within timeout")
		}

		if !strings.Contains(stdoutBuf.String(), "stdout message") || !strings.Contains(stdoutBuf.String(), "delayed message") {
			t.Errorf("Expected custom stdout to contain messages, got: %q", stdoutBuf.String())
		}

		if !strings.Contains(stderrBuf.String(), "stderr message") {
			t.Errorf("Expected custom stderr to contain message, got: %q", stderrBuf.String())
		}
	})

	t.Run("lastLine tracking", func(t *testing.T) {
		p := LongRunning("lastline-test", "bash", Args("-c", "echo 'line 1'; sleep 0.1; echo 'line 2'; sleep 0.1; echo 'final line'"))

		var startErr error
		go func() {
			startErr = p.Start(context.Background())
		}()

		if !waitForState(p, StateRunning, 5*time.Second) {
			t.Fatal("Process failed to start within timeout")
		}

		if startErr != nil {
			t.Fatalf("Failed to start program: %v", startErr)
		}

		if !waitForState(p, StateFinished, 5*time.Second) {
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

		err := p.Start(context.Background())
		if err == nil {
			t.Fatal("Expected error for non-existent command but got nil")
		}

		status := p.Status()
		if !strings.Contains(status, "error") {
			t.Errorf("Expected status to indicate error, got: %q", status)
		}
	})

	t.Run("empty command", func(t *testing.T) {
		p := New("empty", "")

		err := p.Start(context.Background())
		if err == nil {
			t.Fatal("Expected error for empty command but got nil")
		}
	})

	t.Run("large output handling", func(t *testing.T) {
		// Generate ~100KB of output
		p := New("large-output", "bash", Args("-c", "for i in {1..5000}; do echo \"line $i of large output\"; done"))

		err := p.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start program: %v", err)
		}

		output := p.Output()
		if len(output) < 90000 {
			t.Errorf("Expected large output, got only %d bytes", len(output))
		}

		if !strings.Contains(output, "line 5000") {
			t.Errorf("Output should contain final line")
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		p := LongRunning("concurrent-test", "bash", Args("-c", "for i in {1..20}; do echo \"output $i\"; sleep 0.05; done"))

		var startErr error
		go func() {
			startErr = p.Start(context.Background())
		}()

		if !waitForState(p, StateRunning, 5*time.Second) {
			t.Fatal("Process failed to start within timeout")
		}

		if startErr != nil {
			t.Fatalf("Failed to start program: %v", startErr)
		}

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 5; j++ {
					_ = p.Output()
					_ = p.Error()
					_ = p.LastLine()
					_ = p.Status()
					time.Sleep(30 * time.Millisecond)
				}
			}()
		}

		wg.Wait()

		if !waitForState(p, StateFinished, 5*time.Second) {
			t.Fatal("Process failed to complete within timeout")
		}

		if len(p.Output()) == 0 {
			t.Error("Expected some output from concurrent access test")
		}
	})
}

func TestMultiplePrograms(t *testing.T) {
	t.Run("run multiple programs", func(t *testing.T) {
		var programs []Program
		for i := 0; i < 5; i++ {
			p := New(
				fmt.Sprintf("prog-%d", i),
				"echo",
				Args(fmt.Sprintf("output from program %d", i)),
			)
			programs = append(programs, p)
		}

		for _, p := range programs {
			err := p.Start(context.Background())
			if err != nil {
				t.Fatalf("Failed to start program %s: %v", p.Name(), err)
			}
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
