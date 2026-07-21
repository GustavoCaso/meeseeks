package login

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

type Defintion string

// Service interface defines the contract for managing login services
// across different operating systems (macOS, Linux).
type Service interface {
	// Create the service defintion
	Create(ctx context.Context, config ServiceConfig) (Defintion, error)

	// Enable configures the service to start automatically at user login
	Enable(ctx context.Context, service Defintion) error

	// Disable removes the automatic startup configuration
	Disable(ctx context.Context) error

	// Status returns the current status of the login service
	Status(ctx context.Context) (ServiceStatus, error)
}

// ServiceConfig contains the configuration needed to set up
// a login service across different platforms.
type ServiceConfig struct {
	// ConfigPath is the absolute path to the meeseeks configuration file
	ConfigPath string

	// ExecutablePath is the absolute path to the meeseeks executable
	ExecutablePath string

	// ConfigDir is the directory where meeseeks stores its configuration and runtime files
	ConfigDir string
}

// ServiceStatus represents the current state of a login service.
type ServiceStatus struct {
	// Enabled indicates if the service is configured to start at login
	Enabled bool

	// Running indicates if the service is currently running
	Running bool

	// LastRun is the timestamp of the last time the service was started
	LastRun time.Time

	// Error contains any error message if the service is in an error state
	Error string
}

// GetService returns the appropriate Service implementation
// for the current operating system.
func GetService(logger *logger.Logger) Service {
	return getPlatformService(logger)
}

// getLogPath returns the path to the log file for the service.
func getLogPath(logType string) string {
	homeDir, _ := os.UserHomeDir()
	meeseeksDir := os.Getenv("MEESEEKS_CONFIG_DIR")
	if meeseeksDir == "" {
		meeseeksDir = filepath.Join(homeDir, ".config", "meeseeks")
	}
	return filepath.Join(meeseeksDir, fmt.Sprintf("meeseeks.%s.log", logType))
}
