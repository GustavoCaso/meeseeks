package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/internal/server"
)

func reloadCommand(args []string, _ *logger.Logger) error {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	timeout := fs.String(
		"timeout",
		"5s",
		"Timeout to wait for reload operation.",
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks reload [options]\n\n")
		fmt.Fprintf(os.Stderr, "Reload meeseeks configuration\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := server.NewClient(getSocketPath())
	resp, err := client.Reload(ctx, *timeout)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	fmt.Fprintln(os.Stdout, resp.Data)
	return nil
}
