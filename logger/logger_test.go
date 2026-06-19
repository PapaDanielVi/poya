package logger_test

import (
	"testing"

	"github.com/PapaDanielVi/poya/logger"
)

func TestNoopLoggerDoesNotPanic(t *testing.T) {
	t.Parallel()
	l := logger.NewNoop()
	l.Debug("debug", "key", "value")
	l.Info("info")
	l.Warn("warn", "n", 1)
	l.Error("error", "dangling")
}
