package main

import (
	"testing"
)

func TestStopCommand_Validation(t *testing.T) {
	tests := []commandTestCase{
		{
			name:          "stop without program",
			args:          []string{"stop"},
			expectedExit:  1,
			shouldContain: "program name required",
		},
	}

	runCommandTests(t, tests)
}

func TestStopCommand_MissingProgramName(t *testing.T) {
	configContent := `programs:
  - name: "test-stop-program1"
    command: "sleep"
    args: ["30"]
  - name: "test-stop-program2"
    command: "sleep"
    args: ["30"]
`

	newTestServer(t, configContent)

	tests := []commandTestCase{
		{
			name:          "stop with invalid timeout",
			args:          []string{"stop", "-timeout", "invalid", "test-stop-program1"},
			expectedExit:  1,
			shouldContain: "invalid duration",
		},
		{
			name:          "stop non-existing program",
			args:          []string{"stop", "fake-program"},
			expectedExit:  1,
			shouldContain: "program fake-program not present",
		},
		{
			name:          "stop existing program",
			args:          []string{"stop", "test-stop-program1"},
			expectedExit:  0,
			shouldContain: "test-stop-program1 stopped",
		},
		{
			name:          "stop with custom timeout",
			args:          []string{"stop", "-timeout", "10s", "test-stop-program2"},
			expectedExit:  0,
			shouldContain: "test-stop-program2 stopped",
		},
	}

	runCommandTests(t, tests)
}

func TestStopCommand_Help(t *testing.T) {
	testCommandHelp(t, "stop", []string{
		"Usage: meeseeks stop [options] [program_name]",
		"Stop running programs",
		"-timeout",
	})
}
