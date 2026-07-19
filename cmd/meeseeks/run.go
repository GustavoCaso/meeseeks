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
	printLogs := fs.Bool(
		"f",
		false,
		"print logs while the programs run. Otherwise there is no output until the program finish",
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks run [options] <program_name>\n\n")
		fmt.Fprintf(os.Stderr, "Run a single program one-time\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
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

	client := server.NewClient(getSocketPath())

	if !*printLogs {
		resp, err := client.RunProgram(ctx, programName, false)
		if err != nil {
			return err
		}

		if !resp.Success {
			return fmt.Errorf("%s", resp.Error)
		}

		return statusCommand([]string{"-f", "table", programName}, logger)
	}

	resp, err := client.RunProgram(ctx, programName, true)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	return followLogs(ctx, client, programName, false)
}
