package poya

import (
	"encoding/json"
	"reflect"
	"sync/atomic"
)

// DcValue is a dynamically-configured value of type T for dynamic configuration use cases.
// Developers create instances via NewDcValue and pass them around the application.
// Only Get() is public — all mutation is handled internally by the SDK.
//
// For scalar types (string, int, bool, float64, etc.), the SDK parses raw
// provider values via type switch. For struct types, the SDK JSON-decodes
// the raw provider value into T. For slice types ([]string, []int, etc.),
// the SDK JSON-decodes the raw value into a new slice of T.
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
	//nolint:exhaustive // reflect.Kind is not an enum; default handles all other kinds
	switch reflect.TypeOf(defaultValue).Kind() {
	case reflect.Struct:
		d.kind = entryKindStruct
	case reflect.Slice:
		d.kind = entryKindArray
	default:
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

func (d *DcValue[T]) InternalKey(key string) {
	d.key = key
}

// InternalSet updates the current value. Called by the SDK sync loop.

func (d *DcValue[T]) InternalSet(val T) {
	d.val.Store(val)
}

// InternalSetJSON unmarshals raw JSON into T and stores it.
// Called by the SDK sync loop when T is a struct or array type.

func (d *DcValue[T]) InternalSetJSON(raw []byte) error {
	rv := reflect.New(reflect.TypeOf(d.defaultValue))
	if err := json.Unmarshal(raw, rv.Interface()); err != nil {
		return err
	}
	d.val.Store(rv.Elem().Interface())
	return nil
}

// InternalDefault returns the default value. Called by the SDK.

func (d *DcValue[T]) InternalDefault() T {
	return d.defaultValue
}

// InternalAtomic returns the underlying atomic.Value. Called by the SDK.

func (d *DcValue[T]) InternalAtomic() *atomic.Value {
	return &d.val
}

// InternalKind returns the entry kind (scalar, struct, or array). Called by the SDK.

func (d *DcValue[T]) InternalKind() entryKind {
	return d.kind
}

// SetDefaultAndValue sets the default value, current value, and kind
// based on the type of val. The val must be assignable to T. This method
// is intended for use by decode hooks and similar reflection-based
// initialization code.
func (d *DcValue[T]) SetDefaultAndValue(val T) {
	d.defaultValue = val
	d.val.Store(val)
	//nolint:exhaustive // reflect.Kind is not an enum; default handles all other kinds
	switch reflect.TypeOf(val).Kind() {
	case reflect.Struct:
		d.kind = entryKindStruct
	case reflect.Slice:
		d.kind = entryKindArray
	default:
		d.kind = entryKindScalar
	}
}
