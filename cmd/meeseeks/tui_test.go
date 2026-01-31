package main

import (
	"testing"
)

func TestTUICommand_Validation(t *testing.T) {
	setMeeseeksConfigDirForTest(t)

	tests := []commandTestCase{
		{
			name:          "tui with no daemon running",
			args:          []string{"tui"},
			expectedExit:  1,
			shouldContain: "daemon not running",
		},
	}

	runCommandTests(t, tests)
}

func TestTUICommand_Help(t *testing.T) {
	t.Parallel()
	testCommandHelp(t, []string{"tui"}, []string{
		"Usage: meeseeks tui",
		"Launch interactive terminal UI",
	})
}
