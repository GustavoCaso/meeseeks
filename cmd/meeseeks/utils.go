package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"text/tabwriter"

	"github.com/GustavoCaso/meeseeks/pkg/meeseeks"
)

func getMeeseeksDir() string {
	if dir := os.Getenv("MEESEEKS_CONFIG_DIR"); dir != "" {
		return dir
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "meeseeks")
}

func getSocketPath() string {
	return filepath.Join(getMeeseeksDir(), "meeseeks.sock")
}

func getPidFile() string {
	return filepath.Join(getMeeseeksDir(), "meeseeks.pid")
}

func getDefaultConfigPath() string {
	return filepath.Join(getMeeseeksDir(), "config.yaml")
}

func getLogFilePath() string {
	return filepath.Join(getMeeseeksDir(), "meeseeks.log")
}

func getInternalStdoutFile() (*os.File, error) {
	filePath := getLogFilePath()
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
	//nolint:gosec // writing the pid file is ok
	return os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0600)
}

func formatStatisticsAsTable(data any, programName string) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}
	programStatistics := map[string]meeseeks.Statistics{}
	if programName != "" {
		var programStatistic = meeseeks.Statistics{}

		err = json.Unmarshal(jsonBytes, &programStatistic)
		if err != nil {
			return err
		}

		programStatistics[programName] = programStatistic
	} else {
		var programsStatistic = map[string]meeseeks.Statistics{}

		err = json.Unmarshal(jsonBytes, &programsStatistic)
		if err != nil {
			return err
		}

		programStatistics = programsStatistic
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "NAME\tSTATUS\tSUCCESS\tFAILED\tRETRIES\tINTERVAL\tLAST RUN AT\tNEXT RUN\n")
	fmt.Fprintf(w, "----\t------\t-------\t------\t-------\t--------\t-----------\t--------\n")

	for _, stats := range programStatistics {
		name := stats.ProgramName
		successful := stats.Successful
		failed := stats.Failed
		retries := stats.Retries
		lastRunAt := stats.LastRunAt
		nextRunAt := stats.NextRunAt
		interval := "no"

		if stats.Interval != "" {
			interval = stats.Interval
		}

		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%s\t%s\t%s\n",
			truncateString(name, 20),
			stats.State,
			successful,
			failed,
			retries,
			truncateString(interval, 10),
			lastRunAt,
			nextRunAt,
		)
	}

	return w.Flush()
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
