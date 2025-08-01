package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/GustavoCaso/meeseeks/pkg/server"
)

func stopCommand(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks stop [program_name]\n\n")
		fmt.Fprintf(os.Stderr, "Stop running programs\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	programName := ""
	if fs.NArg() > 0 {
		programName = fs.Arg(0)
	}

	client := server.NewClient(getSocketPath())
	resp, err := client.Stop(programName)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	fmt.Fprintln(os.Stdout, "Stop command executed")
	return nil
}
