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
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

func logsCommand(args []string, _ *logger.Logger) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	follow := fs.Bool("f", false, "similar to tail -f")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks logs [options] <program_name>\n\n")
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
			var log program.LogLine
			unmarshalErr := json.Unmarshal(line, &log)

			if unmarshalErr != nil {
				fmt.Fprintf(os.Stderr, "Error unmarshaling log: %s\n", unmarshalErr)
				continue
			}

			if log.IsError {
				fmt.Fprintf(os.Stderr, "%s\n", log.Message)
			} else {
				fmt.Fprintf(os.Stdout, "%s\n", log.Message)
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

	output, ok := response["stdout"].(string)
	if !ok {
		return errors.New("failed to extract stdout information")
	}
	errOutput, ok := response["stderr"].(string)
	if !ok {
		return errors.New("failed to extract stderr information")
	}

	if output != "" {
		fmt.Fprintln(os.Stdout, "---------------------------- Stdout ------------------------------------")
		fmt.Fprintln(os.Stdout, output)
	}

	if errOutput != "" {
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "---------------------------- Stderr ------------------------------------")
		fmt.Fprintln(os.Stdout, errOutput)
	}

	return nil
}
