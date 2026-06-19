package logrus_test

import (
	"io"
	"testing"

	sirupsen "github.com/sirupsen/logrus"

	poyalogrus "github.com/PapaDanielVi/poya/logger/logrus"
)

func TestNewSatisfiesInterfaceAndLogs(t *testing.T) {
	t.Parallel()
	base := sirupsen.New()
	base.SetOutput(io.Discard)
	base.SetLevel(sirupsen.DebugLevel)

	l := poyalogrus.New(base)
	// These calls must not panic, including the odd-length key/value case.
	l.Debug("debug", "key", "value")
	l.Info("info", "n", 1)
	l.Warn("warn")
	l.Error("error", "dangling")
}
