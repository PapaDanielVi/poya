package otel

import (
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/PapaDanielVi/poya/metrics"
)

func TestNew(t *testing.T) {
	t.Parallel()
	meter := noop.Meter{}
	m, err := New(meter)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestIncWatchEvents(t *testing.T) {
	t.Parallel()
	meter := noop.Meter{}
	m, err := New(meter)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	m.IncWatchEvents("key1")
	m.IncWatchEvents("key1")
}

func TestIncWatchErrors(t *testing.T) {
	t.Parallel()
	meter := noop.Meter{}
	m, err := New(meter)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	m.IncWatchErrors("key1")
}

func TestObserveUpdateLatency(t *testing.T) {
	t.Parallel()
	meter := noop.Meter{}
	m, err := New(meter)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	m.ObserveUpdateLatency("key1", 100*time.Millisecond)
}

func TestSetRegisteredKeys(t *testing.T) {
	t.Parallel()
	meter := noop.Meter{}
	m, err := New(meter)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	m.SetRegisteredKeys(5)
}

// Ensure Metrics implements the interface.
var _ metrics.Metrics = (*Metrics)(nil)
