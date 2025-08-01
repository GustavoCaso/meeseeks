package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"text/tabwriter"

	"github.com/GustavoCaso/meeseeks/pkg/config"
	"github.com/GustavoCaso/meeseeks/pkg/program"
)

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
	filePath := filepath.Join(homeDir, ".meeseeks", "meeseeks.log")
	if err := os.MkdirAll(filepath.Dir(filePath), 0750); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func writePidFile(pidFile string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(pidFile), 0750); err != nil {
		return fmt.Errorf("failed to create PID directory: %w", err)
	}
	return os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0600)
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

func formatStatisticsAsTable(data any, programName string) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}
	programStatistics := []program.Statistics{}
	if programName != "" {
		var programStatistic = program.Statistics{}

		err = json.Unmarshal(jsonBytes, &programStatistic)
		if err != nil {
			return err
		}

		programStatistics = append(programStatistics, programStatistic)
	} else {
		var programsStatistic = []program.Statistics{}

		err = json.Unmarshal(jsonBytes, &programsStatistic)
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
			interval = stats.Interval.String()
		}
		status := "idle"
		//nolint:gocritic // using switch would not improve readbility
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
