package metrics

import "time"

// Metrics is the interface for SDK telemetry.
// Use EnableMetrics(true) in Config to enable the real implementation;
// when disabled, a no-op stub is used (no if-checks in hot paths).
type Metrics interface {
	IncWatchEvents(key string)
	IncWatchErrors(key string)
	ObserveUpdateLatency(key string, d time.Duration)
	SetRegisteredKeys(n int)
}

// NoopMetrics is a no-op implementation used when metrics are disabled.
type NoopMetrics struct{}

func (NoopMetrics) IncWatchEvents(_ string)                        {}
func (NoopMetrics) IncWatchErrors(_ string)                        {}
func (NoopMetrics) ObserveUpdateLatency(_ string, _ time.Duration) {}
func (NoopMetrics) SetRegisteredKeys(_ int)                        {}
