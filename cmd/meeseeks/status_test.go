package main

import (
	"testing"
)

func TestStatusCommand(t *testing.T) {
	configContent := `programs:
  - name: "test-echo-detached"
    command: "sleep"
    args: ["30"]
  - name: "interval echo"
    command: "echo"
    args: ["hello world"]
    interval: 12h
`
	newTestServer(t, configContent)

	tests := []commandTestCase{
		{
			name:          "status format table headers",
			args:          []string{"status"},
			expectedExit:  0,
			shouldContain: `NAME                SUCCESS  FAILED  INTERVAL  STATUS    LAST RUN AT          NEXT RUN`,
		},
		{
			name:          "status format table single program",
			args:          []string{"status", "test-echo-detached"},
			expectedExit:  0,
			shouldContain: `test-echo-detached  0        0       no        running`,
		},
		{
			name:          "status format table interval program",
			args:          []string{"status", "interval echo"},
			expectedExit:  0,
			shouldContain: `interval echo  1        0       12h0m0s   finished`,
		},
		{
			name:          "status format json",
			args:          []string{"status", "-f", "json"},
			expectedExit:  0,
			shouldContain: `"program_name": "test-echo-detached"`,
		},
		{
			name:          "status format json single program",
			args:          []string{"status", "-f", "json", "test-echo-detached"},
			expectedExit:  0,
			shouldContain: `"program_name": "test-echo-detached"`,
		},
		{
			name:          "status non existing program",
			args:          []string{"status", "fake"},
			expectedExit:  1,
			shouldContain: `program fake not present`,
		},
	}

	runCommandTests(t, tests)
}

func TestStatusCommand_Validation(t *testing.T) {
	setMeeseeksConfigDirForTest(t)

	tests := []commandTestCase{
		{
			name:          "status with no daemon running",
			args:          []string{"status"},
			expectedExit:  1,
			shouldContain: "meeseeks server not running",
		},
		{
			name:          "status with invalid format",
			args:          []string{"status", "-format", "invalid"},
			expectedExit:  1,
			shouldContain: "invalid format: invalid. Valid formats: table, json",
		},
		{
			name:          "status with invalid format shorthand",
			args:          []string{"status", "-f", "xml"},
			expectedExit:  1,
			shouldContain: "invalid format: xml. Valid formats: table, json",
		},
	}

	runCommandTests(t, tests)
}

func TestStatusCommand_Help(t *testing.T) {
	testCommandHelp(t, []string{"status"}, []string{
		"Usage: meeseeks status [options] [program_name]",
		"Show status of running programs",
		"-format",
		"-f",
	})
}
