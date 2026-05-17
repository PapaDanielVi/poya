// Package poya provides dynamic runtime configuration for Go applications.
// Developers register typed DcValue[T] instances, choose a provider
// (etcd, Redis, or HashiCorp Vault), and the SDK keeps values in sync
// in the background. Developers only call Get() to read the latest value.
package poya

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/PapaDanielVi/poya/provider"
)

// Config holds the SDK initialization options.
type Config struct {
	Provider provider.Provider
	Prefix   string // top-level prefix prepended to all keys
}

// SDK is the main entry point for the poya dynamic config SDK.
type SDK struct {
	mu       sync.RWMutex
	prefix   string
	provider provider.Provider
	values   map[string]*entry
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// entry holds a type-erased reference to a DcValue and its atomic storage.
type entry struct {
	key        string
	defaultVal any
	atomic     *atomic.Value
}

// New creates a new SDK instance with the given configuration.
// Call Register or RegisterStruct next, then Start.
func New(cfg Config) *SDK {
	ctx, cancel := context.WithCancel(context.Background())
	prefix := cfg.Prefix
	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/") + "/"
	}
	return &SDK{
		prefix:   prefix,
		provider: cfg.Provider,
		values:   make(map[string]*entry),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Register adds a single DcValue to the SDK.
// The key is the provider key to watch. If prefix is configured,
// it is prepended to the key automatically.
func Register[T any](s *SDK, key string, val *DcValue[T]) {
	fullKey := s.prefix + key
	val.InternalKey(fullKey)

	s.mu.Lock()
	s.values[fullKey] = &entry{
		key:        fullKey,
		defaultVal: val.InternalDefault(),
		atomic:     val.InternalAtomic(),
	}
	s.mu.Unlock()
}

// RegisterStruct iterates over the fields of the given struct,
// finds DcValue[T] fields, extracts key names from `poya` struct tags,
// and registers them all. Nested structs are handled recursively
// with accumulated parent prefixes. Pass a pointer to the struct so
// that DcValue fields are addressable.
//
// Example:
//
//	type AppConfig struct {
//	    DBHost DcValue[string] `poya:"db_host"`
//	    DBPort DcValue[int]    `poya:"db_port"`
//	}
//	sdk.RegisterStruct(&cfg)
func RegisterStruct(s *SDK, structVal interface{}) {
	v := reflect.ValueOf(structVal)
	if v.Kind() != reflect.Ptr {
		panic("RegisterStruct requires a pointer to a struct")
	}
	registerStruct(s, v.Elem(), "")
}

func registerStruct(s *SDK, v reflect.Value, parentPrefix string) {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		fv := v.Field(i)

		if !fv.CanInterface() {
			continue
		}

		tag := sf.Tag.Get("poya")

		// Check if the field is a DcValue[T] by checking for the Get method
		// and the InternalKey method that all DcValue[T] types share.
		if isDcValue(fv) {
			handleDcValue(s, fv, tag, sf.Name, parentPrefix)
			continue
		}

		// If the field is a struct (but not DcValue), recurse into it
		if fv.Kind() == reflect.Struct || (fv.Kind() == reflect.Ptr && fv.Elem().Kind() == reflect.Struct) {
			nestedPrefix := parentPrefix
			if tag != "" {
				nestedPrefix = parentPrefix + tag + "/"
			}
			registerStruct(s, fv, nestedPrefix)
		}
	}
}

// isDcValue checks if a reflect.Value holds a DcValue[T] by looking for
// the Get() method which is unique to DcValue[T].
func isDcValue(v reflect.Value) bool {
	if !v.CanAddr() {
		return false
	}
	m := reflect.ValueOf(v.Addr().Interface()).MethodByName("Get")
	if !m.IsValid() {
		return false
	}
	// Also verify it has InternalKey to distinguish from other types
	m2 := reflect.ValueOf(v.Addr().Interface()).MethodByName("InternalKey")
	return m2.IsValid()
}

func handleDcValue(s *SDK, fv reflect.Value, tag, fieldName, parentPrefix string) {
	if tag == "" {
		tag = strings.ToLower(fieldName)
	}
	fullKey := s.prefix + parentPrefix + tag

	addr := fv.Addr()
	setKey := reflect.ValueOf(addr.Interface()).MethodByName("InternalKey")
	getDef := reflect.ValueOf(addr.Interface()).MethodByName("InternalDefault")
	getAtomic := reflect.ValueOf(addr.Interface()).MethodByName("InternalAtomic")

	setKey.Call([]reflect.Value{reflect.ValueOf(fullKey)})
	defVal := getDef.Call(nil)[0].Interface()
	atomicPtr := getAtomic.Call(nil)[0].Interface().(*atomic.Value)

	s.mu.Lock()
	s.values[fullKey] = &entry{
		key:        fullKey,
		defaultVal: defVal,
		atomic:     atomicPtr,
	}
	s.mu.Unlock()
}

// Start begins background synchronization for all registered values.
// For each value, it launches a goroutine that watches the provider
// for changes and updates the DcValue atomically.
func (s *SDK) Start() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, e := range s.values {
		s.wg.Add(1)
		go func(ent *entry) {
			defer s.wg.Done()
			_ = s.provider.Watch(s.ctx, ent.key, func(changedKey string, rawValue string) {
				updateEntry(ent, rawValue)
			})
		}(e)
	}
}

// Stop gracefully shuts down all background watchers.
func (s *SDK) Stop() {
	s.cancel()
	s.wg.Wait()
}

// updateEntry attempts to parse the raw string value and update the entry.
// If parsing fails, the value is left unchanged.
func updateEntry(e *entry, raw string) {
	parsed, err := parseValue(raw, e.defaultVal)
	if err != nil {
		return
	}
	e.atomic.Store(parsed)
}

// parseValue converts a raw string to the type of the default value.
func parseValue(raw string, def any) (any, error) {
	switch def.(type) {
	case string:
		return raw, nil
	case int:
		var i int
		_, err := fmt.Sscanf(raw, "%d", &i)
		return i, err
	case int64:
		var i int64
		_, err := fmt.Sscanf(raw, "%d", &i)
		return i, err
	case float64:
		var f float64
		_, err := fmt.Sscanf(raw, "%f", &f)
		return f, err
	case bool:
		return raw == "true" || raw == "1", nil
	default:
		return raw, nil
	}
}
