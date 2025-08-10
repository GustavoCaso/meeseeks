package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"syscall"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

func exitCommand(args []string, _ *logger.Logger) error {
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

	fmt.Fprintln(os.Stdout, "Exiting meeseeks")

	return nil
}
