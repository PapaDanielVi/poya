// Package poya provides dynamic runtime configuration and configuration management
// for Go applications. It supports type-safe generic config values (scalars and structs)
// synced from etcd, Redis, HashiCorp Vault, MySQL, or PostgreSQL backends.
// Developers register DcValue[T] instances and call Get() to read the latest values
// for use cases like feature flags, service discovery, and runtime parameter tuning.
package poya

import (
	"context"
	"encoding"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PapaDanielVi/poya/logger"
	"github.com/PapaDanielVi/poya/metrics"
	prom "github.com/PapaDanielVi/poya/metrics/prometheus"
	"github.com/PapaDanielVi/poya/provider"
)

type poyaTag struct {
	key    string
	prefix string
}

func parsePoyaTag(raw string) poyaTag {
	if raw == "" {
		return poyaTag{}
	}
	var pt poyaTag
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if after, found := strings.CutPrefix(part, "key="); found {
			pt.key = after
			continue
		}
		if after, found := strings.CutPrefix(part, "prefix="); found {
			pt.prefix = after
		}
	}
	return pt
}

type Config struct {
	Provider      provider.Provider
	Prefix        string
	EnableMetrics bool
	Metrics       metrics.Metrics
	Logger        logger.Logger

	// Disabled turns off dynamic configuration entirely. When true, Start() is a
	// no-op: the SDK never connects to a provider, never watches anything, and
	// every registered DcValue keeps its default. A provider is not required in
	// this mode. Use it to ship the same binary with dynamic config switched off.
	Disabled bool
}

type SDK struct {
	mu       sync.RWMutex
	prefix   string
	provider provider.Provider
	values   map[string]*entry
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	metrics  metrics.Metrics
	log      logger.Logger
	disabled bool
}

type entry struct {
	key        string
	defaultVal any
	atomic     *atomic.Value
	kind       EntryKind
}

type EntryKind int

const (
	EntryKindScalar EntryKind = iota
	EntryKindStruct
	EntryKindArray
)

func New(cfg Config) *SDK {
	ctx, cancel := context.WithCancel(context.Background())
	prefix := cfg.Prefix
	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/") + "/"
	}
	var m metrics.Metrics
	switch {
	case cfg.Metrics != nil:
		m = cfg.Metrics
	case cfg.EnableMetrics:
		m = prom.New()
	default:
		m = metrics.NoopMetrics{}
	}
	l := cfg.Logger
	if l == nil {
		l = logger.New()
	}
	return &SDK{
		prefix:   prefix,
		provider: cfg.Provider,
		values:   make(map[string]*entry),
		ctx:      ctx,
		cancel:   cancel,
		metrics:  m,
		log:      l,
		disabled: cfg.Disabled,
	}
}

func Register[T any](s *SDK, key string, val *DcValue[T]) {
	fullKey := s.prefix + key
	val.InternalKey(fullKey)

	s.mu.Lock()
	s.values[fullKey] = &entry{
		key:        fullKey,
		defaultVal: val.InternalDefault(),
		atomic:     val.InternalAtomic(),
		kind:       val.InternalKind(),
	}
	s.mu.Unlock()

	s.log.Debug("registered config value", "key", fullKey, "kind", val.InternalKind())
}

func RegisterConfig(s *SDK, structVal any) {
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
	for i := range t.NumField() {
		sf := t.Field(i)
		fv := v.Field(i)

		if !fv.CanInterface() {
			continue
		}

		tag := parsePoyaTag(sf.Tag.Get("poya"))

		if isDcValue(fv) {
			keyName := tag.key
			if keyName == "" {
				keyName = strings.ToLower(sf.Name)
			}
			fullKey := s.prefix + parentPrefix + keyName
			handleDcValue(s, fv, fullKey)
			continue
		}

		if fv.Kind() == reflect.Struct || (fv.Kind() == reflect.Pointer && fv.Elem().Kind() == reflect.Struct) {
			nestedPrefix := calcNestedPrefix(parentPrefix, tag)
			registerConfig(s, fv, nestedPrefix)
		}
	}
}

func calcNestedPrefix(parent string, tag poyaTag) string {
	prefix := parent
	if tag.prefix != "" {
		prefix = parent + tag.prefix + "/"
	}
	if tag.key != "" {
		prefix = parent + tag.key + "/"
	}
	return prefix
}

func isDcValue(v reflect.Value) bool {
	if !v.CanAddr() {
		return false
	}
	// For *DcValue[T] pointer fields, methods are on the pointer itself.
	// For embedded DcValue[T] value fields, methods are on the address.
	rcv := reflect.ValueOf(v.Addr().Interface())
	if v.Kind() == reflect.Pointer {
		rcv = v
	}
	return rcv.MethodByName("Get").IsValid() &&
		rcv.MethodByName("InternalKey").IsValid() &&
		rcv.MethodByName("InternalSet").IsValid()
}

func handleDcValue(s *SDK, fv reflect.Value, fullKey string) {
	// For *DcValue[T] pointer fields, use the pointer directly.
	// For embedded DcValue[T] value fields, use the address.
	rcv := fv
	if fv.Kind() != reflect.Pointer {
		rcv = reflect.ValueOf(fv.Addr().Interface())
	}

	setKey := rcv.MethodByName("InternalKey")
	getDef := rcv.MethodByName("InternalDefault")
	getAtomic := rcv.MethodByName("InternalAtomic")
	getKind := rcv.MethodByName("InternalKind")

	setKey.Call([]reflect.Value{reflect.ValueOf(fullKey)})
	defVal := getDef.Call(nil)[0].Interface()
	atomicVal := getAtomic.Call(nil)[0].Interface()
	atomicPtr, ok := atomicVal.(*atomic.Value)
	if !ok {
		panic("InternalAtomic must return *atomic.Value")
	}
	kindValI := getKind.Call(nil)[0].Interface()
	kindVal, ok := kindValI.(EntryKind)
	if !ok {
		panic("InternalKind must return EntryKind")
	}

	s.mu.Lock()
	s.values[fullKey] = &entry{
		key:        fullKey,
		defaultVal: defVal,
		atomic:     atomicPtr,
		kind:       kindVal,
	}
	s.mu.Unlock()
}

func (s *SDK) Start() {
	if s.disabled {
		s.log.Info("poya disabled, not watching; registered values stay at their defaults")
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	s.log.Info("starting watcher", "key_count", len(s.values))

	keys := make([]string, 0, len(s.values))
	for k := range s.values {
		keys = append(keys, k)
	}

	s.wg.Add(1)
	go func() { //nolint:modernize // waitgroupgo: standard pattern is acceptable
		defer s.wg.Done()
		s.log.Debug("watcher started", "keys", len(keys))
		err := s.provider.Watch(s.ctx, keys, func(changedKey string, rawValue string) {
			start := time.Now()
			s.metrics.IncWatchEvents(changedKey)
			s.mu.RLock()
			ent, ok := s.values[changedKey]
			s.mu.RUnlock()
			if !ok {
				return
			}
			updateEntry(ent, rawValue)
			s.metrics.ObserveUpdateLatency(changedKey, time.Since(start))
			s.log.Debug("value updated", "key", changedKey)
		}, func(deletedKey string) {
			s.metrics.IncWatchEvents(deletedKey)
			s.mu.RLock()
			ent, ok := s.values[deletedKey]
			s.mu.RUnlock()
			if !ok {
				return
			}
			revertEntry(ent)
			s.log.Debug("value reverted to default", "key", deletedKey)
		})
		if err != nil {
			s.log.Error("watcher error", "error", err)
			s.metrics.IncWatchErrors("provider")
		}
		s.log.Debug("watcher stopped")
	}()
}

func (s *SDK) Stop() {
	s.log.Info("stopping watchers")
	s.cancel()
	s.wg.Wait()
	s.log.Info("all watchers stopped")
}

// revertEntry restores an entry to the default its DcValue was constructed with.
func revertEntry(e *entry) {
	e.atomic.Store(e.defaultVal)
}

func updateEntry(e *entry, raw string) {
	switch e.kind {
	case EntryKindScalar:
		updateScalarEntry(e, raw)
	case EntryKindStruct:
		updateStructEntry(e, raw)
	case EntryKindArray:
		updateArrayEntry(e, raw)
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
	rv := reflect.New(reflect.TypeOf(e.defaultVal))
	if err := json.Unmarshal([]byte(raw), rv.Interface()); err != nil {
		return
	}
	e.atomic.Store(rv.Elem().Interface())
}

func updateArrayEntry(e *entry, raw string) {
	rv := reflect.New(reflect.TypeOf(e.defaultVal))
	if err := json.Unmarshal([]byte(raw), rv.Interface()); err != nil {
		return
	}
	e.atomic.Store(rv.Elem().Interface())
}

// parseValue converts a raw provider string into the concrete type of def.
// It drives off reflect.Kind plus strconv, so every Go scalar kind is handled,
// including named types whose underlying kind is a basic type (e.g. type Level
// int). Types that need bespoke parsing (time.Duration, time.Time, []byte) are
// special-cased first, and any type implementing encoding.TextUnmarshaler parses
// itself. The returned value's dynamic type matches def so it can be stored in
// the same atomic.Value without a type mismatch. On a parse error the caller
// keeps the previous value.
func parseValue(raw string, def any) (any, error) {
	switch def.(type) {
	case time.Duration:
		return time.ParseDuration(raw)
	case time.Time:
		return time.Parse(time.RFC3339, raw)
	case []byte:
		return []byte(raw), nil
	}

	rt := reflect.TypeOf(def)
	if rt == nil {
		return raw, nil
	}

	if parsed, ok, err := parseTextUnmarshaler(raw, rt); ok {
		return parsed, err
	}

	return parseByKind(raw, rt)
}

// parseTextUnmarshaler uses encoding.TextUnmarshaler when the default type's
// pointer implements it, letting domain types parse their own string form. The
// bool reports whether the type was handled at all.
func parseTextUnmarshaler(raw string, rt reflect.Type) (any, bool, error) {
	ptr := reflect.New(rt)
	tu, ok := ptr.Interface().(encoding.TextUnmarshaler)
	if !ok {
		return nil, false, nil
	}
	if err := tu.UnmarshalText([]byte(raw)); err != nil {
		return nil, true, err
	}
	return ptr.Elem().Interface(), true, nil
}

// parseByKind parses raw into the basic kind of rt and converts the result back
// to rt so named types round-trip. Unparseable kinds fall back to the raw string.
func parseByKind(raw string, rt reflect.Type) (any, error) {
	//nolint:exhaustive // only basic scalar kinds are parseable from a string; others fall through.
	switch rt.Kind() {
	case reflect.String:
		return reflect.ValueOf(raw).Convert(rt).Interface(), nil
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, err
		}
		return reflect.ValueOf(b).Convert(rt).Interface(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(raw, 10, rt.Bits())
		if err != nil {
			return nil, err
		}
		return reflect.ValueOf(i).Convert(rt).Interface(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(raw, 10, rt.Bits())
		if err != nil {
			return nil, err
		}
		return reflect.ValueOf(u).Convert(rt).Interface(), nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, rt.Bits())
		if err != nil {
			return nil, err
		}
		return reflect.ValueOf(f).Convert(rt).Interface(), nil
	case reflect.Complex64, reflect.Complex128:
		c, err := strconv.ParseComplex(raw, rt.Bits())
		if err != nil {
			return nil, err
		}
		return reflect.ValueOf(c).Convert(rt).Interface(), nil
	default:
		return raw, nil
	}
}
