package main

import (
	"fmt"
	"os"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

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
	case "run":
		cmdErr = runCommand(args, logger)
	case "status":
		cmdErr = statusCommand(args, logger)
	case "logs":
		cmdErr = logsCommand(args, logger)
	case "stop":
		cmdErr = stopCommand(args, logger)
	case "exit":
		cmdErr = exitCommand(args, logger)
	case "run-at-login":
		cmdErr = runAtLoginCommand(args)
	case "version":
		fmt.Fprintln(os.Stdout, "meeseeks version 1.0.0")
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
	fmt.Fprintf(os.Stderr, "  run           Start programs from config file\n")
	fmt.Fprintf(os.Stderr, "  status        Show status of running programs\n")
	fmt.Fprintf(os.Stderr, "  logs          Show logs for a specific program\n")
	fmt.Fprintf(os.Stderr, "  stop          Stop running programs\n")
	fmt.Fprintf(os.Stderr, "  run-at-login  Manage automatic startup at user login\n")
	fmt.Fprintf(os.Stderr, "  exit          Stop meeseeks program\n")
	fmt.Fprintf(os.Stderr, "  version       Show version information\n")
	fmt.Fprintf(os.Stderr, "\nUse 'meeseeks <command> -h' for more information about a command.\n")
}
