package main

import (
	"testing"
)

func TestLogsCommand_Validation(t *testing.T) {
	// Make sure running tests while having a production
	// instance of meeseeks running do not cause problems
	customDir := "/tmp/meeseeks"
	t.Setenv("MEESEEKS_CONFIG_DIR", customDir)

	tests := []commandTestCase{
		{
			name:          "logs without program name",
			args:          []string{"logs"},
			expectedExit:  1,
			shouldContain: "program name required",
		},
		{
			name:          "logs with no daemon running",
			args:          []string{"logs", "test-program"},
			expectedExit:  1,
			shouldContain: "meeseeks server not running",
		},
	}

	runCommandTests(t, tests)
}

func TestLogsCommand(t *testing.T) {
	// Use a command that produces output so we can test logs functionality
	configContent := `programs:
  - name: "test-echo-logs"
    command: "bash"
    args: ["-c", "echo 'test log message'; sleep 30"]
`

	newTestServer(t, configContent)

	tests := []commandTestCase{
		{
			name:          "logs for existing program",
			args:          []string{"logs", "test-echo-logs"},
			expectedExit:  0,
			shouldContain: "test log message",
		},
		{
			name:          "logs for non-existing program",
			args:          []string{"logs", "fake-program"},
			expectedExit:  1,
			shouldContain: "program not found",
		},
	}

	runCommandTests(t, tests)
}

func TestLogsCommand_Help(t *testing.T) {
	testCommandHelp(t, []string{"logs"}, []string{
		"Usage: meeseeks logs <program_name>",
		"Show logs for a specific program",
	})
}
