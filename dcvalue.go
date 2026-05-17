package poya

import (
	"sync/atomic"
)

// DcValue is a dynamically-configured value of type T.
// Developers create instances via NewDcValue and pass them around the application.
// Only Get() is public — all mutation is handled internally by the SDK.
type DcValue[T any] struct {
	key          string
	defaultValue T
	val          atomic.Value
}

// NewDcValue creates a new DcValue with the given default.
// The key is set later by the SDK during Register / RegisterStruct.
func NewDcValue[T any](defaultValue T) *DcValue[T] {
	d := &DcValue[T]{
		defaultValue: defaultValue,
	}
	d.val.Store(defaultValue)
	return d
}

// Get returns the current value. This is the only method exposed to the developer.
// Reads are lock-free via atomic.Value.
func (d *DcValue[T]) Get() T {
	return d.val.Load().(T)
}

// InternalKey sets the provider key. Called by the SDK during registration.
//nolint:unused
func (d *DcValue[T]) InternalKey(key string) {
	d.key = key
}

// InternalSet updates the current value. Called by the SDK sync loop.
//nolint:unused
func (d *DcValue[T]) InternalSet(val T) {
	d.val.Store(val)
}

// InternalDefault returns the default value. Called by the SDK.
//nolint:unused
func (d *DcValue[T]) InternalDefault() T {
	return d.defaultValue
}

// InternalAtomic returns the underlying atomic.Value. Called by the SDK.
//nolint:unused
func (d *DcValue[T]) InternalAtomic() *atomic.Value {
	return &d.val
}
