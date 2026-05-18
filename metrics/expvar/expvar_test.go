package expvar_test

import (
	"testing"
	"time"

	"github.com/PapaDanielVi/poya/metrics"
	"github.com/PapaDanielVi/poya/metrics/expvar"
)

func TestNew(t *testing.T) {
	t.Parallel()
	m := expvar.New()
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestIncWatchEvents(t *testing.T) {
	t.Parallel()
	m := expvar.New()
	m.IncWatchEvents("key1")
	m.IncWatchEvents("key1")
	m.IncWatchEvents("key2")
}

func TestIncWatchErrors(t *testing.T) {
	t.Parallel()
	m := expvar.New()
	m.IncWatchErrors("key1")
	m.IncWatchErrors("key2")
}

func TestObserveUpdateLatency(t *testing.T) {
	t.Parallel()
	m := expvar.New()
	m.ObserveUpdateLatency("key1", 100*time.Millisecond)
	m.ObserveUpdateLatency("key1", 200*time.Millisecond)
}

func TestSetRegisteredKeys(t *testing.T) {
	t.Parallel()
	m := expvar.New()
	m.SetRegisteredKeys(5)
	m.SetRegisteredKeys(10)
}

// Ensure Metrics implements the interface.
var _ metrics.Metrics = (*expvar.Metrics)(nil)
