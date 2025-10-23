package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/internal/server"
)

type logLine struct {
	Message string `json:"message"`
	Error   bool   `json:"error"`
}

func logsCommand(args []string, _ *logger.Logger) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	follow := fs.Bool("f", false, "similar to tail -f")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks logs <program_name> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Show logs for a specific program\n")
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

	if !*follow {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		client := server.NewClient(ctx, getSocketPath())
		return nonFollowLogs(client, programName)
	}

	// Follow mode
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := server.NewClient(ctx, getSocketPath())
	logsLine := make(chan []byte)

	err := client.FollowLogs(ctx, programName, logsLine)

	if err != nil {
		return err
	}

	done := make(chan struct{})
	go func() {
		defer close(done)

		for line := range logsLine {
			var log logLine
			err := json.Unmarshal(line, &log)

			if err != nil {
				fmt.Fprintf(os.Stderr, "Error unmarshaling log: %s\n", err)
				continue
			}

			if log.Error {
				fmt.Fprintf(os.Stderr, "%s", log.Message)
			} else {
				fmt.Fprintf(os.Stdout, "%s", log.Message)
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan

	cancel()

	<-done

	return nil
}

func nonFollowLogs(client *server.Client, programName string) error {
	resp, err := client.Logs(programName)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	var response = map[string]any{}

	jsonBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return err
	}

	err = json.Unmarshal(jsonBytes, &response)
	if err != nil {
		return err
	}

	output, ok := response["output"].(string)
	if !ok {
		return errors.New("failed to extract output information")
	}
	errOutput, ok := response["error"].(string)
	if !ok {
		return errors.New("failed to extract error information")
	}

	if output != "" {
		fmt.Fprintln(os.Stdout, "---------------------------- OUTPUT ------------------------------------")
		fmt.Fprintln(os.Stdout, output)
	}

	if errOutput != "" {
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "---------------------------- ERROR ------------------------------------")
		fmt.Fprintln(os.Stdout, errOutput)
	}

	return nil
}
