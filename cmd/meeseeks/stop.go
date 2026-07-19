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

func stopCommand(args []string, _ *logger.Logger) error {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	timeout := fs.String(
		"timeout",
		"5s",
		"Timeout to wait for program to exit. If exceeded and error is trigerred and the program is force kill",
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks stop [options] [program_name]\n\n")
		fmt.Fprintf(os.Stderr, "Stop running programs\n")
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
	resp, err := client.Stop(ctx, programName, *timeout)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	fmt.Fprintln(os.Stdout, resp.Data)
	return nil
}
