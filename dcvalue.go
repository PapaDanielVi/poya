package poya

import (
	"encoding/json"
	"reflect"
	"sync/atomic"
)

// DcValue is a dynamically-configured value of type T.
// Developers create instances via NewDcValue and pass them around the application.
// Only Get() is public — all mutation is handled internally by the SDK.
//
// For scalar types (string, int, bool, float64, etc.), the SDK parses raw
// provider values via type switch. For struct types, the SDK JSON-decodes
// the raw provider value into T.
type DcValue[T any] struct {
	key          string
	defaultValue T
	val          atomic.Value
	kind         entryKind
}

// NewDcValue creates a new DcValue with the given default.
// The key is set later by the SDK during Register.
func NewDcValue[T any](defaultValue T) *DcValue[T] {
	d := &DcValue[T]{
		defaultValue: defaultValue,
	}
	d.val.Store(defaultValue)
	if reflect.TypeOf(defaultValue).Kind() == reflect.Struct {
		d.kind = entryKindStruct
	} else {
		d.kind = entryKindScalar
	}
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

// InternalSetJSON unmarshals raw JSON into T and stores it.
// Called by the SDK sync loop when T is a struct type.
//nolint:unused
func (d *DcValue[T]) InternalSetJSON(raw []byte) error {
	rv := reflect.New(reflect.TypeOf(d.defaultValue))
	if err := json.Unmarshal(raw, rv.Interface()); err != nil {
		return err
	}
	d.val.Store(rv.Elem().Interface())
	return nil
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

// InternalKind returns the entry kind (scalar or struct). Called by the SDK.
//nolint:unused
func (d *DcValue[T]) InternalKind() entryKind {
	return d.kind
}
