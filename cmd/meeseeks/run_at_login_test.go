package main

import (
	"testing"
)

func TestRunAtLoginCommand_Help(t *testing.T) {
	testCommandHelp(t, []string{"run-at-login"}, []string{
		"Usage: meeseeks run-at-login <subcommand> [options]",
		"Manage automatic startup of meeseeks at user login",
		"enable",
		"disable",
		"status",
	})
}

func TestRunAtLoginEnableCommand_Help(t *testing.T) {
	testCommandHelp(t, []string{"run-at-login", "enable"}, []string{
		"Usage: meeseeks run-at-login enable",
		"Configure meeseeks to start automatically at login",
	})
}

func TestRunAtLoginDisableCommand_Help(t *testing.T) {
	testCommandHelp(t, []string{"run-at-login", "disable"}, []string{
		"Usage: meeseeks run-at-login disable",
		"Remove automatic startup configuration",
	})
}

func TestRunAtLoginStatusCommand_Help(t *testing.T) {
	testCommandHelp(t, []string{"run-at-login", "status"}, []string{
		"Usage: meeseeks run-at-login status",
		"Show current login service status",
	})
}

func TestRunAtLoginCommand_InvalidSubcommand(t *testing.T) {
	tests := []commandTestCase{
		{
			name:          "no subcommand",
			args:          []string{"run-at-login"},
			expectedExit:  1,
			shouldContain: "subcommand required",
		},
		{
			name:          "invalid subcommand",
			args:          []string{"run-at-login", "invalid"},
			expectedExit:  1,
			shouldContain: "unknown subcommand: invalid",
		},
	}

	runCommandTests(t, tests)
}
