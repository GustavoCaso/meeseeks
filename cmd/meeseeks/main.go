package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/GustavoCaso/meeseeks/pkg/config"
	"github.com/GustavoCaso/meeseeks/pkg/daemon"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "run":
		if err := runCommand(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := statusCommand(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "logs":
		if err := logsCommand(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "stop":
		if err := stopCommand(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "exit":
		if err := exitCommand(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Fprintln(os.Stdout, "meeseeks version 1.0.0")
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: meeseeks <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  run     Start programs from config file\n")
	fmt.Fprintf(os.Stderr, "  status  Show status of running programs\n")
	fmt.Fprintf(os.Stderr, "  logs    Show logs for a specific program\n")
	fmt.Fprintf(os.Stderr, "  stop    Stop running programs\n")
	fmt.Fprintf(os.Stderr, "  version Show version information\n")
	fmt.Fprintf(os.Stderr, "\nUse 'meeseeks <command> -h' for more information about a command.\n")
}

func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configFile := fs.String("config", "", "Path to configuration file (required)")
	detach := fs.Bool("d", false, "Run in detached mode")
	daemonChild := fs.Bool("daemon-child", false, "Internal flag for daemon child process")

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

	sockPath := daemon.GetSocketPath()
	pidFile := daemon.GetPidFile()

	if *daemonChild {
		return runDaemonChild(cfg, sockPath, pidFile)
	}

	if *detach {
		return runDetached(cfg, pidFile)
	}

	return runForeground(cfg, sockPath)
}

func runDetached(cfg *config.Config, pidFile string) error {
	// Prepare arguments for the daemon child process
	args := append(os.Args[1:], "--daemon-child") // Skip os.Args[0] (program name)

	// Create the command to start the daemon
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session to properly detach
	}

	// Redirect all I/O to /dev/null for true daemon behavior
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	// Start the daemon process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	// Write the child PID to the PID file
	if err := writePidFile(pidFile, cmd.Process.Pid); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	slog.Info("Started meeseeks daemon", "pid", cmd.Process.Pid, "program_count", len(cfg.Programs))

	// Release the process reference so we don't hold onto it
	_ = cmd.Process.Release()

	// Parent process exits, returning control to the terminal
	return nil
}

func runDaemonChild(cfg *config.Config, sockPath, pidFile string) error {
	// Close stdin, redirect stdout and stderr to /dev/null for true daemon behavior
	if err := syscall.Close(0); err != nil {
		slog.Warn("Failed to close stdin", "error", err)
	}

	devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		slog.Warn("Failed to open /dev/null", "error", err)
	} else {
		defer devNull.Close()
		syscall.Dup2(int(devNull.Fd()), 1) // stdout
		syscall.Dup2(int(devNull.Fd()), 2) // stderr
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d, err := startDaemon(ctx, cfg, sockPath)
	if err != nil {
		return err
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Received signal, shutting down...")
		_ = d.Stop()
		_ = os.Remove(pidFile)
		cancel()
	}()

	if err := d.Wait(ctx); err != nil {
		slog.Warn("Wait completed with error", "error", err)
	}

	return nil
}

func runForeground(cfg *config.Config, sockPath string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d, err := startDaemon(ctx, cfg, sockPath)
	if err != nil {
		return err
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Received signal, shutting down...")
		_ = d.Stop()
		cancel()
	}()

	fmt.Fprintf(os.Stdout, "Starting %d programs in foreground mode\n", len(cfg.Programs))

	if err := d.Wait(ctx); err != nil {
		slog.Warn("Wait completed with error", "error", err)
	}

	return nil
}

func startDaemon(ctx context.Context, cfg *config.Config, sockPath string) (*daemon.Daemon, error) {
	d := daemon.New(sockPath)

	for _, programConfig := range cfg.Programs {
		prog, err := createProgramFromConfig(programConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create program %s: %w", programConfig.Name, err)
		}

		if addErr := d.AddProgram(prog); addErr != nil {
			return nil, fmt.Errorf("failed to add program %s: %w", programConfig.Name, addErr)
		}
	}

	if err := d.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start daemon: %w", err)
	}

	d.StartPrograms(ctx)

	return d, nil
}

func statusCommand(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks status [program_name]\n\n")
		fmt.Fprintf(os.Stderr, "Show status of running programs\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	programName := ""
	if fs.NArg() > 0 {
		programName = fs.Arg(0)
	}

	client := daemon.NewClient(daemon.GetSocketPath())
	resp, err := client.Status(programName)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	if programName != "" {
		fmt.Fprintln(os.Stdout, resp.Data)
	} else {
		data, _ := json.MarshalIndent(resp.Data, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
	}

	return nil
}

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

	client := daemon.NewClient(daemon.GetSocketPath())
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

	client := daemon.NewClient(daemon.GetSocketPath())
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

func exitCommand(args []string) error {
	fs := flag.NewFlagSet("exit", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: meeseeks exit\n\n")
		fmt.Fprintf(os.Stderr, "Kills meeseeks process\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := daemon.NewClient(daemon.GetSocketPath())
	resp, err := client.Exit()
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	fmt.Fprintln(os.Stdout, "Exit command executed")
	return nil
}

func createProgramFromConfig(pc config.ProgramConfig) (program.Program, error) {
	var opts []program.Option

	if len(pc.Args) > 0 {
		opts = append(opts, program.Args(pc.Args...))
	}

	if len(pc.Env) > 0 {
		opts = append(opts, program.Envs(pc.Env...))
	}

	if pc.KeepStdinOpen {
		opts = append(opts, program.KeepStdinOpen())
	}

	if pc.Interval != "" {
		interval, err := pc.GetInterval()
		if err != nil {
			return nil, fmt.Errorf("invalid interval: %w", err)
		}
		opts = append(opts, program.Interval(interval))
	}

	if pc.Stdout != "" {
		file, err := os.OpenFile(pc.Stdout, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed to open stdout file %s: %w", pc.Stdout, err)
		}
		opts = append(opts, program.Stdout(file))
	}

	if pc.Stderr != "" {
		file, err := os.OpenFile(pc.Stderr, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed to open stderr file %s: %w", pc.Stderr, err)
		}
		opts = append(opts, program.Stderr(file))
	}

	return program.New(pc.Name, pc.Command, opts...), nil
}

func writePidFile(pidFile string, pid int) error {
	return os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0600)
}
