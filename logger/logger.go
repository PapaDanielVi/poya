// Package logger provides a minimal structured-logger interface and a default
// stdlib-based implementation for the poya SDK.
package logger

import (
	"log/slog"
	"os"
)

// Logger is the interface used by the poya SDK for all log output.
// It follows the idiomatic Go pattern (like zap's sugared logger or klog).
type Logger interface {
	Debug(msg string, keysAndValues ...any)
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// New returns a default Logger that writes to stderr using log/slog.
func New() Logger {
	return &slogLogger{
		l: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
}

// NewWithHandler returns a Logger backed by the given slog.Handler.
// Useful for tests or custom formatting.
func NewWithHandler(h slog.Handler) Logger {
	return &slogLogger{l: slog.New(h)}
}

type slogLogger struct {
	l *slog.Logger
}

func (s *slogLogger) Debug(msg string, keysAndValues ...any) {
	s.l.Debug(msg, keysAndValues...)
}

func (s *slogLogger) Info(msg string, keysAndValues ...any) {
	s.l.Info(msg, keysAndValues...)
}

func (s *slogLogger) Warn(msg string, keysAndValues ...any) {
	s.l.Warn(msg, keysAndValues...)
}

func (s *slogLogger) Error(msg string, keysAndValues ...any) {
	s.l.Error(msg, keysAndValues...)
}

// noopLogger is a no-op Logger used when logging is disabled.
type noopLogger struct{}

func (noopLogger) Debug(_ string, _ ...any) {}
func (noopLogger) Info(_ string, _ ...any)  {}
func (noopLogger) Warn(_ string, _ ...any)  {}
func (noopLogger) Error(_ string, _ ...any) {}
