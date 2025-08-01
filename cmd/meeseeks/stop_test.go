package main

import (
	"testing"
)

func TestStopCommand_Validation(t *testing.T) {
	ensureNoDaemonRunning(t)

	tests := []commandTestCase{
		{
			name:          "stop with no daemon running",
			args:          []string{"stop"},
			expectedExit:  1,
			shouldContain: "meeseeks server not running",
		},
		{
			name:          "stop specific program with no daemon",
			args:          []string{"stop", "test-program"},
			expectedExit:  1,
			shouldContain: "meeseeks server not running",
		},
	}

	runCommandTests(t, tests)
}

func TestStopCommand(t *testing.T) {

	configContent := `programs:
  - name: "test-stop-program1"
    command: "sleep"
    args: ["30"]
  - name: "test-stop-program2"
    command: "sleep"
    args: ["30"]
`

	newTestDetachedDaemon(t, configContent)

	tests := []commandTestCase{
		{
			name:          "stop specific program - not implemented",
			args:          []string{"stop", "test-stop-program1"},
			expectedExit:  1,
			shouldContain: "stop command not yet implemented",
		},
		{
			name:          "stop non-existing program - not implemented",
			args:          []string{"stop", "fake-program"},
			expectedExit:  1,
			shouldContain: "stop command not yet implemented",
		},
		{
			name:          "stop all programs - not implemented",
			args:          []string{"stop"},
			expectedExit:  1,
			shouldContain: "stop command not yet implemented",
		},
	}

	runCommandTests(t, tests)
}

func TestStopCommand_Help(t *testing.T) {
	testCommandHelp(t, "stop", []string{
		"Usage: meeseeks stop [program_name]",
		"Stop running programs",
	})
}
