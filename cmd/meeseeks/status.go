package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/GustavoCaso/meeseeks/internal/server"
)

func statusCommand(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	format := fs.String("format", "table", "Output format: table, json")
	fs.StringVar(format, "f", "table", "Output format: table, json (shorthand)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks status [options] [program_name]\n\n")
		fmt.Fprintf(os.Stderr, "Show status of running programs\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	programName := ""
	if fs.NArg() > 0 {
		programName = fs.Arg(0)
	}

	if *format != "table" && *format != "json" {
		return fmt.Errorf("invalid format: %s. Valid formats: table, json", *format)
	}

	client := server.NewClient(getSocketPath())
	resp, err := client.Statistics(programName)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	if *format == "json" {
		data, _ := json.MarshalIndent(resp.Data, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
	} else {
		if err = formatStatisticsAsTable(resp.Data, programName); err != nil {
			return err
		}
	}

	return nil
}
