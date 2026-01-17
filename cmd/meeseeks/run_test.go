package main

import (
	"path/filepath"
	"testing"
)

func TestRunCommand(t *testing.T) {
	configContent := `programs:
  - name: "test-echo"
    command: "echo"
    args: ["hello world"]
  - name: "sleep"
    command: "sleep"
    args: ["10"]
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-config.yaml")
	newTestServer(t, configFile, configContent)

	tests := []commandTestCase{
		{
			name:          "run without program name",
			args:          []string{"run"},
			expectedExit:  1,
			shouldContain: "program name required",
		},
		{
			name:          "run existing",
			args:          []string{"run", "test-echo"},
			expectedExit:  0,
			shouldContain: "test-echo  finished",
		},
		{
			name:          "run running program",
			args:          []string{"run", "sleep"},
			expectedExit:  1,
			shouldContain: "program sleep already running",
		},
		{
			name:          "run nonexistent program",
			args:          []string{"run", "nonexistent"},
			expectedExit:  1,
			shouldContain: "program nonexistent not present",
		},
	}

	runCommandTests(t, tests)
}

func TestRunCommandHelp(t *testing.T) {
	testCommandHelp(t, []string{"run"}, []string{
		"Usage: meeseeks run [options] <program_name>",
		"Run a single program one-time",
		"-f",
	})
}
