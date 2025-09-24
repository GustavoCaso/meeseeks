/*
Meeseks is a process manager that can execute programs once or on intervals,
providing comprehensive monitoring, logging, and management capabilities.

Usage: meeseeks <command> [options]

Commands:

	start          Start programs from config file
	start-at-login Manage automatic startup at user login
	status         Show status of running programs
	run            Run a specific program
	logs           Show logs for a specific program
	stop           Stop a specific program
	exit           Stop meeseeks process
	version        Show version information
*/
package main

import (
	"fmt"
	"os"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

var Version = "development"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]
	var cmdErr error

	logger := logger.New()

	switch command {
	case "start":
		cmdErr = startCommand(args, logger)
	case "start-at-login":
		cmdErr = startAtLoginCommand(args, logger)
	case "status":
		cmdErr = statusCommand(args, logger)
	case "logs":
		cmdErr = logsCommand(args, logger)
	case "run":
		cmdErr = runCommand(args, logger)
	case "stop":
		cmdErr = stopCommand(args, logger)
	case "exit":
		cmdErr = exitCommand(args, logger)
	case "reload":
		cmdErr = reloadCommand(args, logger)
	case "version":
		fmt.Fprintf(os.Stdout, "meeseeks version %s\n", Version)
	case "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}

	if cmdErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", cmdErr)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: meeseeks <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  start          Start programs from config file\n")
	fmt.Fprintf(os.Stderr, "  start-at-login Manage automatic startup at user login\n")
	fmt.Fprintf(os.Stderr, "  status         Show status of running programs\n")
	fmt.Fprintf(os.Stderr, "  run            Run a specific program\n")
	fmt.Fprintf(os.Stderr, "  logs           Show logs for a specific program\n")
	fmt.Fprintf(os.Stderr, "  stop           Stop a specific program\n")
	fmt.Fprintf(os.Stderr, "  exit           Stop meeseeks process\n")
	fmt.Fprintf(os.Stderr, "  version        Show version information\n")
	fmt.Fprintf(os.Stderr, "\nUse 'meeseeks <command> -h' for more information about a command.\n")
}
