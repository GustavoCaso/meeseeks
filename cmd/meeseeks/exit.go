package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"syscall"
)

func exitCommand(args []string) error {
	fs := flag.NewFlagSet("exit", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks exit\n\n")
		fmt.Fprintf(os.Stderr, "Kills meeseeks process\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	pidFile := getPidFile()

	pid, err := os.ReadFile(pidFile)

	if err != nil {
		return err
	}

	pidInt, err := strconv.Atoi(string(pid))
	if err != nil {
		return err
	}

	serverProcess, err := os.FindProcess(pidInt)
	if err != nil {
		return err
	}
	err = serverProcess.Signal(syscall.SIGTERM)
	if err != nil {
		return err
	}
	//nolint:sloglint //currently working on adding support for custom logger
	slog.Info("Exiting meeseeks")

	return nil
}
