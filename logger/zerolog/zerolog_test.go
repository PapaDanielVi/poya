package zerolog_test

import (
	"io"
	"testing"

	rszerolog "github.com/rs/zerolog"

	poyazerolog "github.com/PapaDanielVi/poya/logger/zerolog"
)

func TestNewSatisfiesInterfaceAndLogs(t *testing.T) {
	t.Parallel()
	l := poyazerolog.New(rszerolog.New(io.Discard))
	// These calls must not panic, including the odd-length key/value case.
	l.Debug("debug", "key", "value")
	l.Info("info", "n", 1)
	l.Warn("warn")
	l.Error("error", "dangling")
}
