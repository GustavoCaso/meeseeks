package logger

import (
	"log/slog"
	"os"
)

type Logger struct {
	logger      *slog.Logger
	errorLogger *slog.Logger
}

func New() *Logger {
	regularHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: false,
	})
	errorHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     slog.LevelError,
		AddSource: false,
	})

	return &Logger{
		logger:      slog.New(regularHandler),
		errorLogger: slog.New(errorHandler),
	}
}

func (l *Logger) Debug(msg string, args ...interface{}) {
	l.logger.Debug(msg, args...)
}

func (l *Logger) Info(msg string, args ...interface{}) {
	l.logger.Info(msg, args...)
}

func (l *Logger) Warn(msg string, args ...interface{}) {
	l.logger.Warn(msg, args...)
}

func (l *Logger) Error(msg string, args ...interface{}) {
	l.errorLogger.Error(msg, args...)
}

func (l *Logger) Fatal(msg string, args ...interface{}) {
	l.errorLogger.Error(msg, args...)
	os.Exit(1)
}
