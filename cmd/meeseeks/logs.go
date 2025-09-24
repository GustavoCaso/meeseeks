package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/internal/server"
)

func logsCommand(args []string, _ *logger.Logger) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks logs <program_name>\n\n")
		fmt.Fprintf(os.Stderr, "Show logs for a specific program\n")
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
