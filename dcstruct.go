package poya

import (
	"encoding/json"
	"sync/atomic"
)

// DcStruct is a dynamically-configured struct value of type T.
// Developers create instances via NewDcStruct and pass them around the application.
// The SDK fetches a JSON value from the provider and decodes it into T.
// Only Get() is public — all mutation is handled internally by the SDK.
type DcStruct[T any] struct {
	key          string
	defaultValue T
	val          atomic.Value
}

// NewDcStruct creates a new DcStruct with the given default.
// The key is set later by the SDK during Register / RegisterStruct.
func NewDcStruct[T any](defaultValue T) *DcStruct[T] {
	d := &DcStruct[T]{
		defaultValue: defaultValue,
	}
	d.val.Store(defaultValue)
	return d
}

// Get returns the current value. This is the only method exposed to the developer.
// Reads are lock-free via atomic.Value.
func (d *DcStruct[T]) Get() T {
	return d.val.Load().(T)
}

// InternalKey sets the provider key. Called by the SDK during registration.
func (d *DcStruct[T]) InternalKey(key string) {
	d.key = key
}

// InternalSetJSON unmarshals raw JSON into T and stores it.
// Called by the SDK sync loop when the provider value is a JSON object.
func (d *DcStruct[T]) InternalSetJSON(raw []byte) error {
	var val T
	if err := json.Unmarshal(raw, &val); err != nil {
		return err
	}
	d.val.Store(val)
	return nil
}

// InternalDefault returns the default value. Called by the SDK.
func (d *DcStruct[T]) InternalDefault() T {
	return d.defaultValue
}

// InternalAtomic returns the underlying atomic.Value. Called by the SDK.
func (d *DcStruct[T]) InternalAtomic() *atomic.Value {
	return &d.val
}
