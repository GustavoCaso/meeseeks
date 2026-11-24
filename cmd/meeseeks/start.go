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

const (
	// detachedStartTimeout bounds how long start -d waits for the daemon, which
	// may first need to stop programs left by a crash within their kill grace.
	detachedStartTimeout = time.Minute
	// detachedPollInterval is how often start -d checks the daemon's progress.
	detachedPollInterval = 20 * time.Millisecond
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
	cmd := exec.CommandContext(context.Background(), os.Args[0], "start")
	cmd.Env = os.Environ()
	cmd.SysProcAttr = detachSysProcAttr()

	cmd.Stdin = nil

	var logOffset int64
	stdoutFile, stdoutErr := getInternalStdoutFile()
	if stdoutErr != nil {
		c.logger.Warn(
			"Failed to create log file for meeseeks, using /dev/null",
			"error",
			stdoutErr.Error(),
		)
	} else {
		defer stdoutFile.Close()
		cmd.Stdout = stdoutFile
		cmd.Stderr = stdoutFile
		if info, statErr := stdoutFile.Stat(); statErr == nil {
			logOffset = info.Size()
		}
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start meeseeks process: %w", err)
	}

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	if err := c.waitForDaemon(cmd.Process.Pid, exited, logOffset); err != nil {
		return err
	}

	c.logger.Info("Started meeseeks (detached)", "pid", cmd.Process.Pid)

	return nil
}

// waitForDaemon blocks until the daemon reports it is serving. The daemon
// writes its own PID only after it holds the instance lock and is listening, so
// a matching PID file cannot come from a different, already running daemon.
func (c *cmd) waitForDaemon(pid int, exited <-chan struct{}, logOffset int64) error {
	ticker := time.NewTicker(detachedPollInterval)
	defer ticker.Stop()
	timeout := time.After(detachedStartTimeout)

	for {
		if running, err := readPidFile(c.pidFile); err == nil && running == pid {
			return nil
		}

		select {
		case <-exited:
			return fmt.Errorf(
				"meeseeks failed to start: %s (log: %s)",
				lastLogLineSince(logOffset),
				getLogFilePath(),
			)
		case <-timeout:
			return fmt.Errorf(
				"meeseeks did not become ready within %s (log: %s)",
				detachedStartTimeout,
				getLogFilePath(),
			)
		case <-ticker.C:
		}
	}
}

func (c *cmd) startForeground() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := startServer(ctx, c.configPath, c.logger, c.socketPath, 5*time.Second)
	if err != nil {
		return err
	}

	if err = writePidFile(c.pidFile, os.Getpid()); err != nil {
		_ = s.Stop()
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	go func() {
		c.logger.Info("Started meeseeks")

		if waitErr := s.Wait(ctx); waitErr != nil {
			c.logger.Warn("Wait completed with error", "error", waitErr)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
		c.logger.Info("Received signal, shutting down...")
	case <-s.ShutdownRequested():
		c.logger.Info("Received exit request, shutting down...")
	}

	// Removed while still holding the instance lock, so this cannot delete the
	// PID file of a newer daemon.
	_ = os.Remove(c.pidFile)

	err = s.Stop()
	if err != nil {
		c.logger.Warn("Error stopping the server.", "error", err.Error())
	}

	return nil
}

func startCommand(args []string, logger *logger.Logger) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
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

	configPath := getDefaultConfigPath()
	sockPath := getSocketPath()
	pidFile := getPidFile()

	cmd := &cmd{
		configPath: configPath,
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
