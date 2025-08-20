package logger

import (
	"log/slog"
	"os"
	"time"
)

type Logger struct {
	logger      *slog.Logger
	errorLogger *slog.Logger
}

func New() *Logger {
	replacer := func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			a.Value = slog.StringValue(a.Value.Time().Format(time.RFC822))
		}
		return a
	}

	regularHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		AddSource:   false,
		ReplaceAttr: replacer,
	})
	errorHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       slog.LevelError,
		AddSource:   false,
		ReplaceAttr: replacer,
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
