package program

import (
	"io"
	"time"

	"github.com/GustavoCaso/meeseeks/pkg/logger"
)

// Option defines a function type for configuring program instances.
type Option func(*program)

// StdoutFile redirects program stdout to the specified file path.
// The file will be created if it doesn't exist, with output appended.
func StdoutFile(file string) Option {
	return func(p *program) {
		p.stdoutFile = file
	}
}

// StderrFile redirects program stderr to the specified file path.
// The file will be created if it doesn't exist, with output appended.
func StderrFile(file string) Option {
	return func(p *program) {
		p.stderrFile = file
	}
}

// Stdout redirects program stdout to the provided io.Writer.
// Output will be written to both the internal buffer and the provided writer.
func Stdout(o io.Writer) Option {
	return func(p *program) {
		p.customStdout = o
	}
}

// Stderr redirects program stderr to the provided io.Writer.
// Output will be written to both the internal buffer and the provided writer.
func Stderr(o io.Writer) Option {
	return func(p *program) {
		p.customStderr = o
	}
}

// Stdin provides input to the program from the specified io.Reader.
// Input will be available to the program's stdin in addition to any data sent via Send().
func Stdin(o io.Reader) Option {
	return func(p *program) {
		p.customStdin = o
	}
}

// Args sets the command-line arguments for the program.
// These arguments will be passed to the program when it starts.
func Args(args ...string) Option {
	return func(p *program) {
		p.arguments = args
	}
}

// Envs sets additional environment variables for the program.
// These are added to the current environment and should be in KEY=VALUE format.
func Envs(envs ...string) Option {
	return func(p *program) {
		p.customEnv = envs
	}
}

// KeepStdinOpen keeps the program's stdin pipe open for sending data.
// Required if you plan to use Send() or CloseStdin() methods.
func KeepStdinOpen() Option {
	return func(p *program) {
		p.keepStdinOpen = true
	}
}

// Async configures the program to run asynchronously.
// When async, Start() returns immediately without waiting for completion.
func Async() Option {
	return func(p *program) {
		p.async = true
	}
}

// Logger sets the logger instance for the program.
// The logger will be used for internal logging operations and error reporting.
func Logger(logger logger.Logger) Option {
	return func(p *program) {
		p.logger = logger
	}
}

// Interval sets the interval information for the program
// This information is used by the meeseeks package to schedule the program in a cron-like style.
func Interval(duration time.Duration) Option {
	return func(p *program) {
		p.interval = duration
	}
}

// InitialDelay sets the initial delay information for the program
// This information is used by the meeseeks package.
func InitialDelay(duration time.Duration) Option {
	return func(p *program) {
		p.initialDelay = duration
	}
}

// BufferSizeLimit sets the maximum size in bytes for stdout/stderr buffers.
// When the limit is reached, buffers are truncated to prevent memory issues.
// A limit of 0 means no limit (buffers can grow indefinitely).
func BufferSizeLimit(limit int) Option {
	return func(p *program) {
		p.bufferLimit = limit
	}
}
