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

	logger := logger.New()

	switch command {
	case "run":
		if err := runCommand(args, logger); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := statusCommand(args, logger); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "logs":
		if err := logsCommand(args, logger); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "stop":
		if err := stopCommand(args, logger); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "exit":
		if err := exitCommand(args, logger); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Fprintln(os.Stdout, "meeseeks version 1.0.0")
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: meeseeks <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  run     Start programs from config file\n")
	fmt.Fprintf(os.Stderr, "  status  Show status of running programs\n")
	fmt.Fprintf(os.Stderr, "  logs    Show logs for a specific program\n")
	fmt.Fprintf(os.Stderr, "  stop    Stop running programs\n")
	fmt.Fprintf(os.Stderr, "  version Show version information\n")
	fmt.Fprintf(os.Stderr, "\nUse 'meeseeks <command> -h' for more information about a command.\n")
}
