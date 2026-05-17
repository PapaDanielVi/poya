package poya

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics is the interface for SDK telemetry.
// Use EnableMetrics(true) in Config to enable the real implementation;
// when disabled, a no-op stub is used (no if-checks in hot paths).
type Metrics interface {
	IncWatchEvents(key string)
	IncWatchErrors(key string)
	ObserveUpdateLatency(key string, d time.Duration)
	SetRegisteredKeys(n int)
}

// realMetrics emits Prometheus metrics.
type realMetrics struct {
	watchEvents    *prometheus.CounterVec
	watchErrors    *prometheus.CounterVec
	updateLatency  *prometheus.HistogramVec
	registeredKeys prometheus.Gauge
}

func newRealMetrics() *realMetrics {
	m := &realMetrics{
		watchEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "poya",
			Subsystem: "watch",
			Name:      "events_total",
			Help:      "Total number of watch events received from providers",
		}, []string{"key"}),
		watchErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "poya",
			Subsystem: "watch",
			Name:      "errors_total",
			Help:      "Total number of watch errors from providers",
		}, []string{"key"}),
		updateLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "poya",
			Subsystem: "sync",
			Name:      "update_latency_seconds",
			Help:      "Latency of value updates from provider event to atomic store",
			Buckets:   prometheus.DefBuckets,
		}, []string{"key"}),
		registeredKeys: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "poya",
			Name:      "registered_keys",
			Help:      "Number of registered config keys currently being watched",
		}),
	}
	prometheus.MustRegister(m.watchEvents, m.watchErrors, m.updateLatency, m.registeredKeys)
	return m
}

func (m *realMetrics) IncWatchEvents(key string) {
	m.watchEvents.WithLabelValues(key).Inc()
}

func (m *realMetrics) IncWatchErrors(key string) {
	m.watchErrors.WithLabelValues(key).Inc()
}

func (m *realMetrics) ObserveUpdateLatency(key string, d time.Duration) {
	m.updateLatency.WithLabelValues(key).Observe(d.Seconds())
}

func (m *realMetrics) SetRegisteredKeys(n int) {
	m.registeredKeys.Set(float64(n))
}

// noopMetrics is a no-op implementation used when metrics are disabled.
type noopMetrics struct{}

func (noopMetrics) IncWatchEvents(_ string)                        {}
func (noopMetrics) IncWatchErrors(_ string)                        {}
func (noopMetrics) ObserveUpdateLatency(_ string, _ time.Duration) {}
func (noopMetrics) SetRegisteredKeys(_ int)                        {}
