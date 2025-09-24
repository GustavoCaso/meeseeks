// Package logger defines the logging interface used throughout Meeseeks.
// It provides a simple abstraction for structured logging operations.
package logger

// Logger defines the logging interface used throughout Meeseeks.
type Logger interface {
	// Debug logs debug-level messages with optional key-value context.
	Debug(msg string, args ...any)
	// Info logs informational messages with optional key-value context.
	Info(msg string, args ...any)
	// Warn logs warning messages with optional key-value context.
	Warn(msg string, args ...any)
	// Error logs error messages with optional key-value context.
	Error(msg string, args ...any)
	// Fatal logs fatal error messages and terminates the program.
	Fatal(msg string, args ...any)
}
