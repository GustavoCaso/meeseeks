package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/internal/server"
)

// exitTimeout bounds how long exit waits for programs to stop.
const exitTimeout = time.Minute

func exitCommand(args []string, _ *logger.Logger) error {
	fs := flag.NewFlagSet("exit", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks exit\n\n")
		fmt.Fprintf(os.Stderr, "Stops all programs and exits the meeseeks daemon\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), exitTimeout)
	defer cancel()

	client := server.NewClient(getSocketPath())

	resp, err := client.Shutdown(ctx)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	if err = client.WaitUntilStopped(ctx); err != nil {
		return fmt.Errorf("meeseeks is still stopping: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Exiting meeseeks")

	return nil
}
