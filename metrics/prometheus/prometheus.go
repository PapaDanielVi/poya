package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PapaDanielVi/poya/metrics"
)

// RealMetrics emits Prometheus metrics using a dedicated registry
// to avoid duplicate registration panics across multiple SDK instances.
type RealMetrics struct {
	watchEvents    *prometheus.CounterVec
	watchErrors    *prometheus.CounterVec
	updateLatency  *prometheus.HistogramVec
	registeredKeys prometheus.Gauge
}

// New creates a new RealMetrics instance with its own Prometheus registry.
func New() *RealMetrics {
	reg := prometheus.NewRegistry()
	m := &RealMetrics{
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
	reg.MustRegister(m.watchEvents, m.watchErrors, m.updateLatency, m.registeredKeys)
	return m
}

func (m *RealMetrics) IncWatchEvents(key string) {
	m.watchEvents.WithLabelValues(key).Inc()
}

func (m *RealMetrics) IncWatchErrors(key string) {
	m.watchErrors.WithLabelValues(key).Inc()
}

func (m *RealMetrics) ObserveUpdateLatency(key string, d time.Duration) {
	m.updateLatency.WithLabelValues(key).Observe(d.Seconds())
}

func (m *RealMetrics) SetRegisteredKeys(n int) {
	m.registeredKeys.Set(float64(n))
}

// Ensure RealMetrics implements the Metrics interface.
var _ metrics.Metrics = (*RealMetrics)(nil)
