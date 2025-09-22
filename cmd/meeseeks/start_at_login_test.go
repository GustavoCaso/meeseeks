package main

import (
	"testing"
)

func TestStartAtLoginCommand_Help(t *testing.T) {
	t.Parallel()
	testCommandHelp(t, []string{"start-at-login"}, []string{
		"Usage: meeseeks start-at-login <subcommand>",
		"Manage automatic startup of meeseeks at user login",
		"enable",
		"disable",
		"status",
	})
}

func TestStartAtLoginEnableCommand_Help(t *testing.T) {
	t.Parallel()
	testCommandHelp(t, []string{"start-at-login", "enable"}, []string{
		"Usage: meeseeks start-at-login enable",
		"Configure meeseeks to start automatically at login",
	})
}

func TestStartAtLoginDisableCommand_Help(t *testing.T) {
	t.Parallel()
	testCommandHelp(t, []string{"start-at-login", "disable"}, []string{
		"Usage: meeseeks start-at-login disable",
		"Remove automatic startup configuration",
	})
}

func TestStartAtLoginStatusCommand_Help(t *testing.T) {
	t.Parallel()
	testCommandHelp(t, []string{"start-at-login", "status"}, []string{
		"Usage: meeseeks start-at-login status",
		"Show current login service status",
	})
}

func TestStartAtLoginCommand_InvalidSubcommand(t *testing.T) {
	tests := []commandTestCase{
		{
			name:          "no subcommand",
			args:          []string{"start-at-login"},
			expectedExit:  1,
			shouldContain: "subcommand required",
		},
		{
			name:          "invalid subcommand",
			args:          []string{"start-at-login", "invalid"},
			expectedExit:  1,
			shouldContain: "unknown subcommand: invalid",
		},
	}

	runCommandTests(t, tests)
}
