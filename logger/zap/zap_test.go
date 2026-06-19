package zap_test

import (
	"testing"

	uberzap "go.uber.org/zap"

	poyazap "github.com/PapaDanielVi/poya/logger/zap"
)

func TestNewSatisfiesInterfaceAndLogs(t *testing.T) {
	t.Parallel()
	l := poyazap.New(uberzap.NewNop())
	// A no-op zap logger discards output; these calls must not panic, including
	// the odd-length key/value case.
	l.Debug("debug", "key", "value")
	l.Info("info", "n", 1)
	l.Warn("warn")
	l.Error("error", "dangling")
}
