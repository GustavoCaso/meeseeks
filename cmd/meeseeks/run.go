package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/GustavoCaso/meeseeks/pkg/config"
	"github.com/GustavoCaso/meeseeks/pkg/server"
)

func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configFile := fs.String("config", "", "Path to configuration file (required)")
	detach := fs.Bool("d", false, "Run in detached mode")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks run [options]\n\n")
		fmt.Fprintf(os.Stderr, "Start programs from configuration file\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -config flag is required\n\n")
		fs.Usage()
		return errors.New("config file required")
	}

	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	sockPath := getSocketPath()
	pidFile := getPidFile()

	if *detach {
		return runDetached(pidFile, *configFile, cfg)
	}

	return runForeground(cfg, sockPath, pidFile)
}

func runDetached(pidFile, configFile string, cfg *config.Config) error {
	//nolint:gosec // the arguments are provided by the user
	cmd := exec.Command(os.Args[0], "run", "-config", configFile)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	cmd.Stdin = nil
	stdoutFile, stdoutErr := getInternalStdoutFile()
	if stdoutErr != nil {
		//nolint:sloglint //currently working on adding support for custom logger
		slog.Warn("Failed to create log file for meeseeks, using /dev/null", "error", stdoutErr.Error())
		cmd.Stdout = nil
		cmd.Stderr = nil
	}
	cmd.Stdout = stdoutFile
	cmd.Stderr = stdoutFile

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start meeseeks process: %w", err)
	}

	if err := writePidFile(pidFile, cmd.Process.Pid); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	//nolint:sloglint //currently working on adding support for custom logger
	slog.Info("Started meeseeks (detached)", "pid", cmd.Process.Pid, "program_count", len(cfg.Programs))

	_ = cmd.Process.Release()

	return nil
}

func runForeground(cfg *config.Config, sockPath, pidFile string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := startServer(ctx, cfg, sockPath)
	if err != nil {
		return err
	}

	go func() {
		slog.Info("Started meeseeks", "program_count", len(cfg.Programs))

		if waitErr := s.Wait(ctx); waitErr != nil {
			slog.Warn("Wait completed with error", "error", waitErr)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	//nolint:sloglint //currently working on adding support for custom logger
	slog.Info("Received signal, shutting down...")
	err = s.Stop()
	if err != nil {
		//nolint:sloglint //currently working on adding support for custom logger
		slog.Warn("Error stopping the server.", "error", err.Error())
	}
	_ = os.Remove(pidFile)

	return nil
}

func startServer(ctx context.Context, cfg *config.Config, sockPath string) (*server.Server, error) {
	s := server.New(sockPath)

	for _, programConfig := range cfg.Programs {
		prog, err := createProgramFromConfig(programConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create program %s: %w", programConfig.Name, err)
		}

		if addErr := s.AddProgram(prog); addErr != nil {
			return nil, fmt.Errorf("failed to add program %s: %w", programConfig.Name, addErr)
		}
	}

	if err := s.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start daemon: %w", err)
	}

	s.StartPrograms(ctx)

	return s, nil
}
