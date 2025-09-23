package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/internal/server"
)

func runCommand(args []string, logger *logger.Logger) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks run <program_name>\n\n")
		fmt.Fprintf(os.Stderr, "Run a single program one-time\n\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		fs.Usage()
		return errors.New("program name required")
	}

	programName := fs.Arg(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := server.NewClient(ctx, getSocketPath())
	resp, err := client.RunProgram(programName)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	return statusCommand([]string{"-f", "table", programName}, logger)
}
