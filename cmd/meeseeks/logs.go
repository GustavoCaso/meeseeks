package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/GustavoCaso/meeseeks/internal/server"
)

func logsCommand(args []string) error {
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

	client := server.NewClient(getSocketPath())
	resp, err := client.Logs(programName)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	data, _ := json.MarshalIndent(resp.Data, "", "  ")
	fmt.Fprintln(os.Stdout, string(data))

	return nil
}
