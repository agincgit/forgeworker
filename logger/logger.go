// Package logger provides a centralized logging setup using zerolog.
// It supports both JSON and console output formats and can be configured
// via the Config struct.
package logger

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Logger is the package-level logger instance that can be used throughout
// the application. It is initialized with sensible defaults.
var Logger zerolog.Logger

func init() {
	// Initialize with default settings (can be overridden via Setup)
	Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
}

// Setup configures the global logger with the specified level and format.
// This should be called early in application startup.
func Setup(level string, format string) {
	Logger = New(level, format, os.Stderr)
}

// New creates a new zerolog.Logger with the specified configuration.
// This is useful for consumers who want to create their own logger instance.
func New(level string, format string, w io.Writer) zerolog.Logger {
	// Parse log level
	lvl := parseLevel(level)
	zerolog.SetGlobalLevel(lvl)

	var output io.Writer = w

	// Configure output format
	if strings.ToLower(format) == "console" {
		output = zerolog.ConsoleWriter{
			Out:        w,
			TimeFormat: time.RFC3339,
		}
	}

	return zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Logger()
}

// parseLevel converts a string log level to zerolog.Level.
func parseLevel(level string) zerolog.Level {
	switch strings.ToLower(level) {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

// WithComponent returns a logger with a component field set.
// Useful for identifying which part of the application logged a message.
func WithComponent(component string) zerolog.Logger {
	return Logger.With().Str("component", component).Logger()
}

// WithWorkerID returns a logger with the worker_id field set.
func WithWorkerID(workerID string) zerolog.Logger {
	return Logger.With().Str("worker_id", workerID).Logger()
}
