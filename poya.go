// Package poya provides dynamic runtime configuration and configuration management
// for Go applications. It supports type-safe generic config values (scalars and structs)
// synced from etcd, Redis, HashiCorp Vault, MySQL, or PostgreSQL backends.
// Developers register DcValue[T] instances and call Get() to read the latest values
// for use cases like feature flags, service discovery, and runtime parameter tuning.
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

func parseSigned(raw string) (int64, error) {
	var i int64
	_, err := fmt.Sscanf(raw, "%d", &i)
	return i, err
}

func parseUnsigned(raw string) (uint64, error) {
	var u uint64
	_, err := fmt.Sscanf(raw, "%d", &u)
	return u, err
}

func parseValue(raw string, def any) (any, error) {
	switch def.(type) {
	case string:
		return raw, nil
	case int:
		i, err := parseSigned(raw)
		return int(i), err
	case int8:
		i, err := parseSigned(raw)
		return int8(i), err //nolint:gosec,nolintlint
	case int16:
		i, err := parseSigned(raw)
		return int16(i), err //nolint:gosec,nolintlint
	case int32:
		i, err := parseSigned(raw)
		return int32(i), err //nolint:gosec,nolintlint
	case int64:
		return parseSigned(raw)
	case uint:
		u, err := parseUnsigned(raw)
		return uint(u), err
	case uint8:
		u, err := parseUnsigned(raw)
		return uint8(u), err //nolint:gosec,nolintlint
	case uint16:
		u, err := parseUnsigned(raw)
		return uint16(u), err //nolint:gosec,nolintlint
	case uint32:
		u, err := parseUnsigned(raw)
		return uint32(u), err //nolint:gosec,nolintlint
	case uint64:
		return parseUnsigned(raw)
	case float32:
		var f float64
		_, err := fmt.Sscanf(raw, "%f", &f)
		return float32(f), err
	case float64:
		var f float64
		_, err := fmt.Sscanf(raw, "%f", &f)
		return f, err
	case bool:
		return raw == "true" || raw == "1", nil
	case time.Duration:
		return time.ParseDuration(raw)
	case []byte:
		return []byte(raw), nil
	default:
		return raw, nil
	}
}
