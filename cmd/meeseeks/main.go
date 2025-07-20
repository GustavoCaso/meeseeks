package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/GustavoCaso/meeseeks/pkg/config"
	"github.com/GustavoCaso/meeseeks/pkg/daemon"
	"github.com/GustavoCaso/meeseeks/pkg/meeseeks"
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
	case "version":
		fmt.Println("meeseeks version 1.0.0")
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
		return fmt.Errorf("config file required")
	}

	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	sockPath := daemon.GetSocketPath()
	pidFile := daemon.GetPidFile()

	if *detach {
		return runDetached(cfg, sockPath, pidFile)
	}

	return runForeground(cfg)
}

func runDetached(cfg *config.Config, sockPath, pidFile string) error {
	d := daemon.New(sockPath)

	for _, programConfig := range cfg.Programs {
		prog, err := createProgramFromConfig(programConfig)
		if err != nil {
			return fmt.Errorf("failed to create program %s: %w", programConfig.Name, err)
		}

		if err := d.AddProgram(prog); err != nil {
			return fmt.Errorf("failed to add program %s: %w", programConfig.Name, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	if err := writePidFile(pidFile); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
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

	slog.Info("Starting meeseeks daemon", "program_count", len(cfg.Programs))
	d.StartPrograms(ctx)

	if err := d.Wait(ctx); err != nil {
		slog.Warn("Wait completed with error", "error", err)
	}

	return nil
}

func runForeground(cfg *config.Config) error {
	m := meeseeks.New()

	for _, programConfig := range cfg.Programs {
		prog, err := createProgramFromConfig(programConfig)
		if err != nil {
			return fmt.Errorf("failed to create program %s: %w", programConfig.Name, err)
		}

		if err := m.AddProgram(prog); err != nil {
			return fmt.Errorf("failed to add program %s: %w", programConfig.Name, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Received signal, shutting down...")
		cancel()
	}()

	fmt.Printf("Starting %d programs in foreground mode\n", len(cfg.Programs))
	m.Start(ctx)

	if err := m.Wait(ctx); err != nil {
		slog.Warn("Wait completed with error", "error", err)
	}

	fmt.Println("\n=== Program Statistics ===")
	for _, stat := range m.Statistics() {
		fmt.Println(stat.String())
	}

	m.Results(os.Stdout)
	return nil
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
		fmt.Println(resp.Data)
	} else {
		data, _ := json.MarshalIndent(resp.Data, "", "  ")
		fmt.Println(string(data))
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
		return fmt.Errorf("program name required")
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
	fmt.Println(string(data))

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

	fmt.Println("Stop command executed")
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
		file, err := os.OpenFile(pc.Stdout, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open stdout file %s: %w", pc.Stdout, err)
		}
		opts = append(opts, program.Stdout(file))
	}

	if pc.Stderr != "" {
		file, err := os.OpenFile(pc.Stderr, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open stderr file %s: %w", pc.Stderr, err)
		}
		opts = append(opts, program.Stderr(file))
	}

	return program.New(pc.Name, pc.Command, opts...), nil
}

func writePidFile(pidFile string) error {
	pid := os.Getpid()
	return os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644)
}
