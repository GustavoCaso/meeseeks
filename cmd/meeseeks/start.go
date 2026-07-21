package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/GustavoCaso/meeseeks/internal/logger"
	"github.com/GustavoCaso/meeseeks/internal/server"
)

type cmd struct {
	configPath string
	pidFile    string
	socketPath string
	logger     *logger.Logger
	detach     bool
}

func (c *cmd) start() error {
	if c.detach {
		return c.startDetached()
	}

	return c.startForeground()
}

func (c *cmd) startDetached() error {
	//nolint:gosec // the arguments are provided by the user
	cmd := exec.CommandContext(context.Background(), os.Args[0], "start", "-config", c.configPath)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = detachSysProcAttr()

	cmd.Stdin = nil
	stdoutFile, stdoutErr := getInternalStdoutFile()
	if stdoutErr != nil {
		c.logger.Warn(
			"Failed to create log file for meeseeks, using /dev/null",
			"error",
			stdoutErr.Error(),
		)
		cmd.Stdout = nil
		cmd.Stderr = nil
	}
	cmd.Stdout = stdoutFile
	cmd.Stderr = stdoutFile

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start meeseeks process: %w", err)
	}

	if err := writePidFile(c.pidFile, cmd.Process.Pid); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	c.logger.Info("Started meeseeks (detached)", "pid", cmd.Process.Pid)

	_ = cmd.Process.Release()

	return nil
}

func (c *cmd) startForeground() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := startServer(ctx, c.configPath, c.logger, c.socketPath, 5*time.Second)
	if err != nil {
		return err
	}

	go func() {
		c.logger.Info("Started meeseeks")

		if waitErr := s.Wait(ctx); waitErr != nil {
			c.logger.Warn("Wait completed with error", "error", waitErr)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	c.logger.Info("Received signal, shutting down...")
	err = s.Stop()
	if err != nil {
		c.logger.Warn("Error stopping the server.", "error", err.Error())
	}
	_ = os.Remove(c.pidFile)

	return nil
}

func startCommand(args []string, logger *logger.Logger) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	configPath := fs.String(
		"config",
		"",
		"Path to configuration file (defaults to $MEESEEKS_CONFIG_DIR/config.yaml or ~/.config/meeseeks/config.yaml)",
	)
	detach := fs.Bool("d", false, "Start in detached mode")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks start [options]\n\n")
		fmt.Fprintf(os.Stderr, "Start programs from configuration file\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *configPath == "" {
		*configPath = getDefaultConfigPath()
	}

	sockPath := getSocketPath()
	pidFile := getPidFile()

	cmd := &cmd{
		configPath: *configPath,
		pidFile:    pidFile,
		socketPath: sockPath,
		logger:     logger,
		detach:     *detach,
	}

	return cmd.start()
}

func startServer(
	ctx context.Context,
	configPath string,
	logger *logger.Logger,
	sockPath string,
	timeout time.Duration,
) (*server.Server, error) {
	s, err := server.New(sockPath, configPath, logger, timeout)

	if err != nil {
		return nil, err
	}

	if startErr := s.Start(ctx); startErr != nil {
		return nil, fmt.Errorf("failed to start server: %w", startErr)
	}

	return s, nil
}
