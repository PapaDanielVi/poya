// Package poya provides dynamic runtime configuration for Go applications.
// Developers register typed DcValue[T] and DcStruct[T] instances, choose a
// provider (etcd, Redis, or HashiCorp Vault), and the SDK keeps values in sync
// in the background. Developers only call Get() to read the latest value.
package poya

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PapaDanielVi/poya/provider"
)

// poyaTag holds the parsed result of a `poya` struct tag.
// Format: `poya:"key=db_host"` for a value field,
//
//	`poya:"prefix=db"` for a nested-struct prefix,
//	`poya:"key=db_host,prefix=db"` for both.
type poyaTag struct {
	key    string
	prefix string
}

func parsePoyaTag(raw string) poyaTag {
	if raw == "" {
		return poyaTag{}
	}
	var pt poyaTag
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "key=") {
			pt.key = strings.TrimPrefix(part, "key=")
		} else if strings.HasPrefix(part, "prefix=") {
			pt.prefix = strings.TrimPrefix(part, "prefix=")
		}
	}
	return pt
}

// Config holds the SDK initialization options.
type Config struct {
	Provider      provider.Provider
	Prefix        string // top-level prefix prepended to all keys
	EnableMetrics bool   // when true, Prometheus metrics are collected
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
	metrics  Metrics
}

// entry holds a type-erased reference to a config value and its atomic storage.
// kind determines how raw provider values are decoded.
type entry struct {
	key        string
	defaultVal any
	atomic     *atomic.Value
	kind       entryKind
}

type entryKind int

const (
	entryKindScalar entryKind = iota // DcValue[T] — decode via parseValue
	entryKindStruct                  // DcStruct[T] — decode via json.Unmarshal
)

// New creates a new SDK instance with the given configuration.
// Call Register or RegisterStruct next, then Start.
func New(cfg Config) *SDK {
	ctx, cancel := context.WithCancel(context.Background())
	prefix := cfg.Prefix
	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/") + "/"
	}
	var m Metrics
	if cfg.EnableMetrics {
		m = newRealMetrics()
	} else {
		m = noopMetrics{}
	}
	return &SDK{
		prefix:   prefix,
		provider: cfg.Provider,
		values:   make(map[string]*entry),
		ctx:      ctx,
		cancel:   cancel,
		metrics:  m,
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
		kind:       entryKindScalar,
	}
	s.mu.Unlock()
}

// RegisterStruct adds a single DcStruct to the SDK.
// The value is JSON-decoded from the provider on each update.
func RegisterStruct[T any](s *SDK, key string, val *DcStruct[T]) {
	fullKey := s.prefix + key
	val.InternalKey(fullKey)

	s.mu.Lock()
	s.values[fullKey] = &entry{
		key:        fullKey,
		defaultVal: val.InternalDefault(),
		atomic:     val.InternalAtomic(),
		kind:       entryKindStruct,
	}
	s.mu.Unlock()
}

// RegisterConfig iterates over the fields of the given struct,
// finds DcValue[T] and DcStruct[T] fields, extracts key names and prefixes
// from `poya` struct tags, and registers them all. Nested structs are handled
// recursively with accumulated parent prefixes. Pass a pointer to the struct
// so that fields are addressable.
//
// Tag format:
//   - `poya:"key=db_host"` — this field is a config value watched at "db_host"
//   - `poya:"prefix=db"` — this nested struct contributes "db/" to child key paths
//   - `poya:"key=host,prefix=db"` — both a value and a prefix for deeper nesting
//
// Example:
//
//	type AppConfig struct {
//	    DBHost DcValue[string] `poya:"key=db_host"`
//	    DBPort DcValue[int]    `poya:"key=db_port"`
//	    DB     DBConfig        `poya:"prefix=db"`
//	}
//	sdk.RegisterConfig(&cfg)
func RegisterConfig(s *SDK, structVal interface{}) {
	v := reflect.ValueOf(structVal)
	if v.Kind() != reflect.Pointer {
		panic("RegisterConfig requires a pointer to a struct")
	}
	registerConfig(s, v.Elem(), "")
}

func registerConfig(s *SDK, v reflect.Value, parentPrefix string) {
	if v.Kind() == reflect.Pointer {
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

		tag := parsePoyaTag(sf.Tag.Get("poya"))

		// Check if the field is a DcValue[T]
		if isDcValue(fv) {
			keyName := tag.key
			if keyName == "" {
				keyName = strings.ToLower(sf.Name)
			}
			fullKey := s.prefix + parentPrefix + keyName
			handleDcValue(s, fv, fullKey)
			continue
		}

		// Check if the field is a DcStruct[T]
		if isDcStruct(fv) {
			keyName := tag.key
			if keyName == "" {
				keyName = strings.ToLower(sf.Name)
			}
			fullKey := s.prefix + parentPrefix + keyName
			handleDcStruct(s, fv, fullKey)
			continue
		}

		// If the field is a struct (but not DcValue/DcStruct), recurse into it
		if fv.Kind() == reflect.Struct || (fv.Kind() == reflect.Pointer && fv.Elem().Kind() == reflect.Struct) {
			nestedPrefix := parentPrefix
			if tag.prefix != "" {
				nestedPrefix = parentPrefix + tag.prefix + "/"
			}
			if tag.key != "" {
				// This field acts as both a prefix anchor and a nested struct
				nestedPrefix = parentPrefix + tag.key + "/"
			}
			registerConfig(s, fv, nestedPrefix)
		}
	}
}

// isDcValue checks if a reflect.Value holds a DcValue[T] by looking for
// the Get() and InternalKey() methods.
func isDcValue(v reflect.Value) bool {
	if !v.CanAddr() {
		return false
	}
	addr := reflect.ValueOf(v.Addr().Interface())
	return addr.MethodByName("Get").IsValid() &&
		addr.MethodByName("InternalKey").IsValid() &&
		addr.MethodByName("InternalSet").IsValid()
}

// isDcStruct checks if a reflect.Value holds a DcStruct[T] by looking for
// the Get() and InternalSetJSON() methods.
func isDcStruct(v reflect.Value) bool {
	if !v.CanAddr() {
		return false
	}
	addr := reflect.ValueOf(v.Addr().Interface())
	return addr.MethodByName("Get").IsValid() &&
		addr.MethodByName("InternalKey").IsValid() &&
		addr.MethodByName("InternalSetJSON").IsValid()
}

func handleDcValue(s *SDK, fv reflect.Value, fullKey string) {
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
		kind:       entryKindScalar,
	}
	s.mu.Unlock()
}

func handleDcStruct(s *SDK, fv reflect.Value, fullKey string) {
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
		kind:       entryKindStruct,
	}
	s.mu.Unlock()
}

// Start begins background synchronization for all registered values.
// For each value, it launches a goroutine that watches the provider
// for changes and updates the value atomically.
func (s *SDK) Start() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, e := range s.values {
		s.wg.Add(1)
		go func(ent *entry) {
			defer s.wg.Done()
			_ = s.provider.Watch(s.ctx, ent.key, func(changedKey string, rawValue string) {
				start := time.Now()
				s.metrics.IncWatchEvents(changedKey)
				updateEntry(ent, rawValue)
				s.metrics.ObserveUpdateLatency(changedKey, time.Since(start))
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
// Scalar entries use parseValue; struct entries use json.Unmarshal.
func updateEntry(e *entry, raw string) {
	switch e.kind {
	case entryKindStruct:
		updateStructEntry(e, raw)
	default:
		updateScalarEntry(e, raw)
	}
}

func updateScalarEntry(e *entry, raw string) {
	parsed, err := parseValue(raw, e.defaultVal)
	if err != nil {
		return
	}
	e.atomic.Store(parsed)
}

func updateStructEntry(e *entry, raw string) {
	// Create a new instance of the same type as the default value,
	// unmarshal into it, then store. This preserves the concrete type
	// so atomic.Value doesn't panic from type mismatch.
	rv := reflect.New(reflect.TypeOf(e.defaultVal))
	if err := json.Unmarshal([]byte(raw), rv.Interface()); err != nil {
		return
	}
	e.atomic.Store(rv.Elem().Interface())
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
