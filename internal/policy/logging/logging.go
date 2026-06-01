// Package logging provides structured logging initialization for the application.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type contextKey struct{}

// Init configures the default slog logger with the specified level and JSON output.
func Init(level string) {
	var logLevel slog.Level
	var unrecognizedLevel bool
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
		unrecognizedLevel = true
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	slog.SetDefault(slog.New(handler))

	if unrecognizedLevel {
		slog.Warn("Unrecognized log level", level, "using 'info'")
	}
}

// WithLogger returns a new context with the given logger stored in it.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext returns the logger stored in ctx, or the default logger if none is present.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
