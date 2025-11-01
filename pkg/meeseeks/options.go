package meeseeks

import "github.com/GustavoCaso/meeseeks/pkg/logger"

// Option defines a function type for configuring meeseeks instances.
type Option func(*meeseek)

// Logger sets the logger instance for the meeseeks manager.
// The logger will be used for all internal logging operations.
func Logger(logger logger.Logger) Option {
	return func(m *meeseek) {
		m.logger = logger
	}
}
