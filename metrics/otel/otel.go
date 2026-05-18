// Package otel implements the poya Metrics interface using OpenTelemetry.
package otel

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/PapaDanielVi/poya/metrics"
)

// Metrics emits OpenTelemetry metrics via the global meter provider.
type Metrics struct {
	watchEvents    metric.Int64Counter
	watchErrors    metric.Int64Counter
	updateLatency  metric.Float64Histogram
	registeredKeys metric.Int64Gauge
}

// New creates a Metrics instance using the given meter.
// Pass a meter from the global meter provider, e.g.:
//
//	otel.Meter("github.com/PapaDanielVi/poya")
func New(meter metric.Meter) (*Metrics, error) {
	watchEvents, err := meter.Int64Counter(
		"poya.watch.events",
		metric.WithDescription("Total number of watch events received from providers"),
	)
	if err != nil {
		return nil, err
	}

	watchErrors, err := meter.Int64Counter(
		"poya.watch.errors",
		metric.WithDescription("Total number of watch errors from providers"),
	)
	if err != nil {
		return nil, err
	}

	updateLatency, err := meter.Float64Histogram(
		"poya.sync.update_latency",
		metric.WithDescription("Latency of value updates from provider event to atomic store"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	registeredKeys, err := meter.Int64Gauge(
		"poya.registered_keys",
		metric.WithDescription("Number of registered config keys currently being watched"),
	)
	if err != nil {
		return nil, err
	}

	return &Metrics{
		watchEvents:    watchEvents,
		watchErrors:    watchErrors,
		updateLatency:  updateLatency,
		registeredKeys: registeredKeys,
	}, nil
}

// IncWatchEvents increments the counter of watch events for the given key.
func (m *Metrics) IncWatchEvents(key string) {
	m.watchEvents.Add(context.Background(), 1, metric.WithAttributes(attribute.String("key", key)))
}

// IncWatchErrors increments the counter of watch errors for the given key.
func (m *Metrics) IncWatchErrors(key string) {
	m.watchErrors.Add(context.Background(), 1, metric.WithAttributes(attribute.String("key", key)))
}

// ObserveUpdateLatency records the latency of a value update.
func (m *Metrics) ObserveUpdateLatency(key string, d time.Duration) {
	m.updateLatency.Record(context.Background(), d.Seconds(), metric.WithAttributes(attribute.String("key", key)))
}

// SetRegisteredKeys sets the gauge of currently registered keys.
func (m *Metrics) SetRegisteredKeys(n int) {
	m.registeredKeys.Record(context.Background(), int64(n))
}

// Ensure Metrics implements the interface.
var _ metrics.Metrics = (*Metrics)(nil)
