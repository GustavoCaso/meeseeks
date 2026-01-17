package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReload(t *testing.T) {
	configContent := `programs:
  - name: "test-stop-program1"
    command: "sleep"
    args: ["30"]
  - name: "test-stop-program2"
    command: "sleep"
    args: ["30"]
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-config.yaml")
	newTestServer(t, configFile, configContent)

	tests := []commandTestCase{
		{
			name:          "reload with invalid timeout",
			args:          []string{"reload", "-timeout", "invalid"},
			expectedExit:  1,
			shouldContain: "error parsing timeout",
		},
		{
			name:          "get status of current programs",
			args:          []string{"status"},
			expectedExit:  0,
			shouldContain: "test-stop-program1  running  0        0       0        no",
		},
	}

	runCommandTests(t, tests)

	newConfigContent := `programs:
  - name: "test-stop-program2"
    command: "sleep"
    args: ["30"]
  - name: "test-stop-program3"
    command: "echo"
    args: ["Hello"]
`

	if err := os.WriteFile(configFile, []byte(newConfigContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	newTests := []commandTestCase{
		{
			name:          "reload",
			args:          []string{"reload"},
			expectedExit:  0,
			shouldContain: "meeseek configuration reloaded!",
		},
		{
			name:             "get status of current programs after reload",
			args:             []string{"status"},
			expectedExit:     0,
			shouldNotContain: "test-stop-program1",
			shouldContain:    "test-stop-program2",
		},
	}

	runCommandTests(t, newTests)
}

func TestReloadCommand_Help(t *testing.T) {
	t.Parallel()
	testCommandHelp(t, []string{"reload"}, []string{
		"Usage: meeseeks reload [options]",
		"Reload meeseeks configuration",
		"-timeout",
	})
}
