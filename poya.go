// Package poya provides dynamic runtime configuration for Go applications.
// Developers register typed DcValue[T] instances, choose a provider (etcd,
// Redis, HashiCorp Vault, MySQL, or PostgreSQL), and the SDK keeps values in
// sync in the background. Developers only call Get() to read the latest value.
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
	kind       entryKind
}

type entryKind int

const (
	entryKindScalar entryKind = iota
	entryKindStruct
)

func New(cfg Config) *SDK {
	ctx, cancel := context.WithCancel(context.Background())
	prefix := cfg.Prefix
	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/") + "/"
	}
	var m metrics.Metrics
	if cfg.Metrics != nil {
		m = cfg.Metrics
	} else if cfg.EnableMetrics {
		m = prom.New()
	} else {
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
			nestedPrefix := parentPrefix
			if tag.prefix != "" {
				nestedPrefix = parentPrefix + tag.prefix + "/"
			}
			if tag.key != "" {
				nestedPrefix = parentPrefix + tag.key + "/"
			}
			registerConfig(s, fv, nestedPrefix)
		}
	}
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
	atomicPtr := getAtomic.Call(nil)[0].Interface().(*atomic.Value)
	kindVal := getKind.Call(nil)[0].Interface().(entryKind)

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

	s.log.Info("starting watchers", "count", len(s.values))

	for _, e := range s.values {
		s.wg.Add(1)
		go func(ent *entry) {
			defer s.wg.Done()
			s.log.Debug("watcher started", "key", ent.key)
			err := s.provider.Watch(s.ctx, ent.key, func(changedKey string, rawValue string) {
				start := time.Now()
				s.metrics.IncWatchEvents(changedKey)
				updateEntry(ent, rawValue)
				s.metrics.ObserveUpdateLatency(changedKey, time.Since(start))
				s.log.Debug("value updated", "key", changedKey)
			})
			if err != nil {
				s.log.Error("watcher error", "key", ent.key, "error", err)
				s.metrics.IncWatchErrors(ent.key)
			}
			s.log.Debug("watcher stopped", "key", ent.key)
		}(e)
	}
}

func (s *SDK) Stop() {
	s.log.Info("stopping watchers")
	s.cancel()
	s.wg.Wait()
	s.log.Info("all watchers stopped")
}

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
	rv := reflect.New(reflect.TypeOf(e.defaultVal))
	if err := json.Unmarshal([]byte(raw), rv.Interface()); err != nil {
		return
	}
	e.atomic.Store(rv.Elem().Interface())
}

func parseValue(raw string, def any) (any, error) {
	switch def.(type) {
	case string:
		return raw, nil
	case int:
		var i int
		_, err := fmt.Sscanf(raw, "%d", &i)
		return i, err
	case int8:
		var i int64
		_, err := fmt.Sscanf(raw, "%d", &i)
		return int8(i), err
	case int16:
		var i int64
		_, err := fmt.Sscanf(raw, "%d", &i)
		return int16(i), err
	case int32:
		var i int64
		_, err := fmt.Sscanf(raw, "%d", &i)
		return int32(i), err
	case int64:
		var i int64
		_, err := fmt.Sscanf(raw, "%d", &i)
		return i, err
	case uint:
		var u uint64
		_, err := fmt.Sscanf(raw, "%d", &u)
		return uint(u), err
	case uint8:
		var u uint64
		_, err := fmt.Sscanf(raw, "%d", &u)
		return uint8(u), err
	case uint16:
		var u uint64
		_, err := fmt.Sscanf(raw, "%d", &u)
		return uint16(u), err
	case uint32:
		var u uint64
		_, err := fmt.Sscanf(raw, "%d", &u)
		return uint32(u), err
	case uint64:
		var u uint64
		_, err := fmt.Sscanf(raw, "%d", &u)
		return u, err
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
