// Package expvar implements the poya Metrics interface using Go's expvar package.
// This backend requires no external dependencies.
package expvar

import (
	"expvar"
	"sync"
	"time"

	"github.com/PapaDanielVi/poya/metrics"
)

// Metrics emits metrics via Go's built-in expvar package.
// All metrics are published under the "poya" namespace and are accessible
// via the default /debug/vars endpoint when using net/http/pprof or expvar HTTP handler.
type Metrics struct {
	mu             sync.Mutex
	watchEvents    *expvar.Map
	watchErrors    *expvar.Map
	updateLatency  *expvar.Map
	registeredKeys *expvar.Int
}

// New creates a Metrics instance with expvar counters.
func New() *Metrics {
	return &Metrics{
		watchEvents:    publishOrGetMap("poya.events"),
		watchErrors:    publishOrGetMap("poya.errors"),
		updateLatency:  publishOrGetMap("poya.latency"),
		registeredKeys: publishOrGetInt("poya.registered_keys"),
	}
}

// publishOrGetMap publishes a new expvar.Map if one with the given name
// does not already exist, and returns it. If it exists, returns the existing one.
func publishOrGetMap(name string) *expvar.Map {
	if v := expvar.Get(name); v != nil {
		if m, ok := v.(*expvar.Map); ok {
			return m
		}
	}
	m := new(expvar.Map).Init()
	expvar.Publish(name, m)
	return m
}

// publishOrGetInt publishes a new expvar.Int if one with the given name
// does not already exist, and returns it. If it exists, returns the existing one.
func publishOrGetInt(name string) *expvar.Int {
	if v := expvar.Get(name); v != nil {
		if i, ok := v.(*expvar.Int); ok {
			return i
		}
	}
	return expvar.NewInt(name)
}

// IncWatchEvents increments the counter of watch events for the given key.
func (m *Metrics) IncWatchEvents(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchEvents.Add(key, 1)
}

// IncWatchErrors increments the counter of watch errors for the given key.
func (m *Metrics) IncWatchErrors(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchErrors.Add(key, 1)
}

// ObserveUpdateLatency records the latency of a value update.
// The latency in milliseconds is accumulated per key.
func (m *Metrics) ObserveUpdateLatency(key string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateLatency.Add(key, int64(d/time.Millisecond))
}

// SetRegisteredKeys sets the gauge of currently registered keys.
func (m *Metrics) SetRegisteredKeys(n int) {
	m.registeredKeys.Set(int64(n))
}

// Ensure Metrics implements the interface.
var _ metrics.Metrics = (*Metrics)(nil)
