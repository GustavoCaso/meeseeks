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
	"path/filepath"
	"strconv"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/config"
	"github.com/GustavoCaso/meeseeks/pkg/program"
	"github.com/GustavoCaso/meeseeks/pkg/server"
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
		if err := statisticsCommand(args); err != nil {
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
	// Create the command to start the daemon
	cmd := exec.Command(os.Args[0], "run", "-config", configFile)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session to properly detach
	}

	// Redirect all Stdin to /dev/null
	cmd.Stdin = nil
	// Stdout and Stderr use a custom file to have some traceability when running in detached mode
	stdoutFile, stdoutErr := getInternalStdoutFile()
	if stdoutErr != nil {
		slog.Warn("Failed to create log file for meeseeks, using /dev/null", "error", stdoutErr.Error())
		cmd.Stdout = nil
		cmd.Stderr = nil
	}
	cmd.Stdout = stdoutFile
	cmd.Stderr = stdoutFile

	// Start the run process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start meeseeks process: %w", err)
	}

	// Write the child PID to the PID file
	if err := writePidFile(pidFile, cmd.Process.Pid); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	slog.Info("Started meeseeks (detached)", "pid", cmd.Process.Pid, "program_count", len(cfg.Programs))

	// Release the process reference so we don't hold onto it
	_ = cmd.Process.Release()

	// Parent process exits, returning control to the terminal
	return nil
}

func runForeground(cfg *config.Config, sockPath, pidFile string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := startServer(ctx, cfg, sockPath)
	if err != nil {
		return err
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Received signal, shutting down...")
		err := s.Stop()
		if err != nil {
			slog.Warn("Error stopping the server.", "error", err.Error())
		}
		_ = os.Remove(pidFile)
		cancel()
	}()

	slog.Info("Started meeseeks", "program_count", len(cfg.Programs))

	if err := s.Wait(ctx); err != nil {
		slog.Warn("Wait completed with error", "error", err)
	}

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

func statisticsCommand(args []string) error {
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
		if programName != "" {
			fmt.Fprintln(os.Stdout, resp.Data)
		} else {
			data, _ := json.MarshalIndent(resp.Data, "", "  ")
			fmt.Fprintln(os.Stdout, string(data))
		}
	} else {
		if err := formatStatisticsAsTable(resp.Data, programName); err != nil {
			return err
		}
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

	slog.Info("Exiting meeseeks")

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

func getSocketPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".meeseeks", "meeseeks.sock")
}

func getPidFile() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".meeseeks", "meeseeks.pid")
}

func getInternalStdoutFile() (*os.File, error) {
	homeDir, _ := os.UserHomeDir()
	filepath := filepath.Join(homeDir, ".meeseeks", "meeseeks.log")
	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func formatStatisticsAsTable(data any, programName string) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}
	programStatistics := []program.Statistics{}
	if programName != "" {
		var programStatistic = program.Statistics{}

		err := json.Unmarshal(jsonBytes, &programStatistic)
		if err != nil {
			return err
		}

		programStatistics = append(programStatistics, programStatistic)
	} else {
		var programsStatistic = []program.Statistics{}

		err := json.Unmarshal(jsonBytes, &programsStatistic)
		if err != nil {
			return err
		}

		programStatistics = append(programStatistics, programsStatistic...)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "NAME\tRUNS\tSUCCESS\tFAILED\tRUNNING\tINTERVAL\tSTATUS\n")
	fmt.Fprintf(w, "----\t----\t-------\t------\t-------\t--------\t------\n")

	for _, stats := range programStatistics {
		name := stats.ProgramName
		totalRuns := stats.TotalRuns
		successful := stats.Successful
		failed := stats.Failed
		running := stats.Running
		interval := "no"
		if stats.HasInterval {
			interval = time.Duration(stats.Interval).String()
		}
		status := "idle"
		if running > 0 {
			status = "running"
		} else if failed > 0 && successful == 0 {
			status = "failed"
		} else if successful > 0 {
			status = "completed"
		}

		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%s\t%s\n",
			truncateString(name, 20),
			totalRuns,
			successful,
			failed,
			running,
			truncateString(interval, 10),
			status)
	}

	return w.Flush()
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
