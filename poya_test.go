//nolint:testpackage // tests access unexported methods (InternalKind, InternalSet, etc.)
package poya

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/PapaDanielVi/poya/metrics"
	prom "github.com/PapaDanielVi/poya/metrics/prometheus"
	"github.com/PapaDanielVi/poya/provider"
)

type mockProvider struct {
	mu      sync.Mutex
	values  map[string]string
	watchFn func(ctx context.Context, keys []string, onChange func(key string, value string)) error
}

func newMockProvider() *mockProvider {
	return &mockProvider{
		values: make(map[string]string),
	}
}

func (m *mockProvider) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[key], nil
}

func (m *mockProvider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
	if m.watchFn != nil {
		return m.watchFn(ctx, keys, onChange)
	}
	m.mu.Lock()
	for _, key := range keys {
		val := m.values[key]
		if val != "" {
			onChange(key, val)
		}
	}
	m.mu.Unlock()
	<-ctx.Done()
	return nil
}

func (m *mockProvider) Close() error {
	return nil
}

func (m *mockProvider) set(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = value
}

func TestNewSDK(t *testing.T) {
	t.Parallel()
	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: "test/"})
	if sdk == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNewSDKWithMetrics(t *testing.T) {
	t.Parallel()
	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: "test/", EnableMetrics: true})
	if sdk == nil {
		t.Fatal("New() returned nil")
	}
	if _, ok := sdk.metrics.(*prom.RealMetrics); !ok {
		t.Error("expected *prom.RealMetrics when EnableMetrics is true")
	}
}

func TestNewSDKWithoutMetrics(t *testing.T) {
	t.Parallel()
	p := newMockProvider()
	sdk := New(Config{Provider: p})
	if _, ok := sdk.metrics.(metrics.NoopMetrics); !ok {
		t.Error("expected metrics.NoopMetrics when EnableMetrics is false")
	}
}

func TestNewSDKWithCustomMetrics(t *testing.T) {
	t.Parallel()
	p := newMockProvider()
	fm := &fakeMetrics{}
	sdk := New(Config{Provider: p, Metrics: fm})
	if sdk.metrics != fm {
		t.Error("expected custom metrics to be used")
	}
}

func TestRegisterAndGet(t *testing.T) {
	t.Parallel()
	p := newMockProvider()
	p.set("test/mykey", "hello")

	sdk := New(Config{Provider: p, Prefix: "test/"})
	val := NewDcValue("default")
	Register(sdk, "mykey", val)

	if got := val.Get(); got != "default" {
		t.Errorf("Get() = %v, want %v", got, "default")
	}
}

func TestRegisterConfigScalar(t *testing.T) {
	t.Parallel()
	type AppConfig struct {
		DBHost  DcValue[string] `poya:"key=db_host"`
		DBPort  DcValue[int]    `poya:"key=db_port"`
		Verbose DcValue[bool]   `poya:"key=verbose"`
	}

	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: "myapp/"})

	cfg := AppConfig{
		DBHost:  *NewDcValue("localhost"),
		DBPort:  *NewDcValue(5432),
		Verbose: *NewDcValue(false),
	}

	RegisterConfig(sdk, &cfg)

	sdk.mu.RLock()
	defer sdk.mu.RUnlock()

	if _, ok := sdk.values["myapp/db_host"]; !ok {
		t.Error("db_host not registered")
	}
	if _, ok := sdk.values["myapp/db_port"]; !ok {
		t.Error("db_port not registered")
	}
	if _, ok := sdk.values["myapp/verbose"]; !ok {
		t.Error("verbose not registered")
	}
}

func TestRegisterConfigNested(t *testing.T) {
	t.Parallel()
	type DBConfig struct {
		Host DcValue[string] `poya:"key=host"`
		Port DcValue[int]    `poya:"key=port"`
	}
	type AppConfig struct {
		DB DBConfig `poya:"prefix=db"`
	}

	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: "myapp/"})

	cfg := AppConfig{
		DB: DBConfig{
			Host: *NewDcValue("localhost"),
			Port: *NewDcValue(5432),
		},
	}

	RegisterConfig(sdk, &cfg)

	sdk.mu.RLock()
	defer sdk.mu.RUnlock()

	if _, ok := sdk.values["myapp/db/host"]; !ok {
		t.Error("nested db/host not registered")
	}
	if _, ok := sdk.values["myapp/db/port"]; !ok {
		t.Error("nested db/port not registered")
	}
}

func TestRegisterConfigNoTag(t *testing.T) {
	t.Parallel()
	type AppConfig struct {
		MyKey DcValue[string]
	}

	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: ""})

	cfg := AppConfig{
		MyKey: *NewDcValue("default"),
	}

	RegisterConfig(sdk, &cfg)

	sdk.mu.RLock()
	defer sdk.mu.RUnlock()

	if _, ok := sdk.values["mykey"]; !ok {
		t.Error("field without tag should use lowercased field name")
	}
}

func TestRegisterConfigStruct(t *testing.T) {
	t.Parallel()
	type DBConfig struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: "myapp/"})

	val := NewDcValue(DBConfig{Host: "localhost", Port: 5432})
	Register(sdk, "db", val)

	sdk.mu.RLock()
	defer sdk.mu.RUnlock()

	e, ok := sdk.values["myapp/db"]
	if !ok {
		t.Fatal("db struct not registered")
	}
	if e.kind != entryKindStruct {
		t.Error("expected entryKindStruct for struct-typed DcValue")
	}
}

func TestRegisterConfigMixed(t *testing.T) {
	t.Parallel()
	type DBDetails struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	type AppConfig struct {
		Timeout DcValue[time.Duration] `poya:"key=timeout"`
		DB      DcValue[DBDetails]     `poya:"key=db_config"`
	}

	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: "myapp/"})

	cfg := AppConfig{
		Timeout: *NewDcValue(5 * time.Second),
		DB:      *NewDcValue(DBDetails{Host: "localhost", Port: 5432}),
	}

	RegisterConfig(sdk, &cfg)

	sdk.mu.RLock()
	defer sdk.mu.RUnlock()

	timeoutEntry, ok := sdk.values["myapp/timeout"]
	if !ok {
		t.Fatal("timeout not registered")
	}
	if timeoutEntry.kind != entryKindScalar {
		t.Error("expected entryKindScalar for DcValue[time.Duration]")
	}

	dbEntry, ok := sdk.values["myapp/db_config"]
	if !ok {
		t.Fatal("db_config not registered")
	}
	if dbEntry.kind != entryKindStruct {
		t.Error("expected entryKindStruct for DcValue[DBDetails]")
	}
}

func TestStartUpdatesScalarValue(t *testing.T) {
	t.Parallel()
	p := newMockProvider()
	p.set("test/key", "initial")

	updateCh := make(chan struct{}, 1)
	p.watchFn = func(ctx context.Context, keys []string, onChange func(key string, value string)) error {
		for _, key := range keys {
			if key == "test/key" {
				onChange(key, "initial")
				go func() {
					time.Sleep(50 * time.Millisecond)
					onChange(key, "updated")
					updateCh <- struct{}{}
				}()
			}
		}
		<-ctx.Done()
		return nil
	}

	sdk := New(Config{Provider: p, Prefix: "test/"})
	val := NewDcValue("default")
	Register(sdk, "key", val)

	sdk.Start()

	select {
	case <-updateCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for update signal")
	}

	time.Sleep(10 * time.Millisecond)

	if got := val.Get(); got != "updated" {
		t.Errorf("Get() = %v, want %v", got, "updated")
	}

	sdk.Stop()
}

func TestStartUpdatesStructValue(t *testing.T) {
	t.Parallel()
	type DBConfig struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	p := newMockProvider()

	updateCh := make(chan struct{}, 1)
	p.watchFn = func(ctx context.Context, keys []string, onChange func(key string, value string)) error {
		for _, key := range keys {
			if key == "test/db" {
				onChange(key, `{"host":"localhost","port":5432}`)
				go func() {
					time.Sleep(50 * time.Millisecond)
					onChange(key, `{"host":"remote","port":3306}`)
					updateCh <- struct{}{}
				}()
			}
		}
		<-ctx.Done()
		return nil
	}

	sdk := New(Config{Provider: p, Prefix: "test/"})
	val := NewDcValue(DBConfig{})
	Register(sdk, "db", val)

	sdk.Start()

	select {
	case <-updateCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for update signal")
	}

	time.Sleep(10 * time.Millisecond)

	got := val.Get()
	if got.Host != "remote" || got.Port != 3306 {
		t.Errorf("Get() = %+v, want {remote 3306}", got)
	}

	sdk.Stop()
}

func TestStartUpdatesArrayValue(t *testing.T) {
	t.Parallel()
	p := newMockProvider()

	updateCh := make(chan struct{}, 1)
	p.watchFn = func(ctx context.Context, keys []string, onChange func(key string, value string)) error {
		for _, key := range keys {
			if key == "test/ports" {
				onChange(key, `[8080, 9090]`)
				go func() {
					time.Sleep(50 * time.Millisecond)
					onChange(key, `[3000, 4000, 5000]`)
					updateCh <- struct{}{}
				}()
			}
		}
		<-ctx.Done()
		return nil
	}

	sdk := New(Config{Provider: p, Prefix: "test/"})
	val := NewDcValue([]int{})
	Register(sdk, "ports", val)

	sdk.Start()

	select {
	case <-updateCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for update signal")
	}

	time.Sleep(10 * time.Millisecond)

	got := val.Get()
	want := []int{3000, 4000, 5000}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get() = %v, want %v", got, want)
	}

	sdk.Stop()
}

func TestStartUpdatesArrayStringValue(t *testing.T) {
	t.Parallel()
	p := newMockProvider()

	updateCh := make(chan struct{}, 1)
	p.watchFn = func(ctx context.Context, keys []string, onChange func(key string, value string)) error {
		for _, key := range keys {
			if key == "test/tags" {
				onChange(key, `["alpha","beta"]`)
				go func() {
					time.Sleep(50 * time.Millisecond)
					onChange(key, `["gamma","delta","epsilon"]`)
					updateCh <- struct{}{}
				}()
			}
		}
		<-ctx.Done()
		return nil
	}

	sdk := New(Config{Provider: p, Prefix: "test/"})
	val := NewDcValue([]string{})
	Register(sdk, "tags", val)

	sdk.Start()

	select {
	case <-updateCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for update signal")
	}

	time.Sleep(10 * time.Millisecond)

	got := val.Get()
	want := []string{"gamma", "delta", "epsilon"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get() = %v, want %v", got, want)
	}

	sdk.Stop()
}

func TestRegisterConfigArray(t *testing.T) {
	t.Parallel()
	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: "myapp/"})

	val := NewDcValue([]string{})
	Register(sdk, "tags", val)

	sdk.mu.RLock()
	defer sdk.mu.RUnlock()

	e, ok := sdk.values["myapp/tags"]
	if !ok {
		t.Fatal("tags array not registered")
	}
	if e.kind != entryKindArray {
		t.Errorf("expected entryKindArray (%d), got %d", entryKindArray, e.kind)
	}
}

func TestCustomMetricsReceiveEvents(t *testing.T) {
	t.Parallel()
	type DBConfig struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	p := newMockProvider()
	fm := &fakeMetrics{}

	updateCh := make(chan struct{}, 1)
	p.watchFn = func(ctx context.Context, keys []string, onChange func(key string, value string)) error {
		for _, key := range keys {
			if key == "test/db" {
				onChange(key, `{"host":"localhost","port":5432}`)
				go func() {
					time.Sleep(50 * time.Millisecond)
					onChange(key, `{"host":"remote","port":3306}`)
					updateCh <- struct{}{}
				}()
			}
		}
		<-ctx.Done()
		return nil
	}

	sdk := New(Config{Provider: p, Prefix: "test/", Metrics: fm})
	val := NewDcValue(DBConfig{})
	Register(sdk, "db", val)

	sdk.Start()

	select {
	case <-updateCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for update signal")
	}

	time.Sleep(10 * time.Millisecond)
	sdk.Stop()

	if fm.watchEvents == 0 {
		t.Error("expected watch events to be recorded in custom metrics")
	}
	if fm.updateLatency == 0 {
		t.Error("expected update latency to be recorded in custom metrics")
	}
}

func TestParseValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  string
		def  any
		want any
	}{
		{"hello", "default", "hello"},
		{"42", 0, 42},
		{"3.14", 0.0, 3.14},
		{"true", false, true},
		{"1", false, true},
		{"false", true, false},
		{"127", int8(0), int8(127)},
		{"1000", int16(0), int16(1000)},
		{"100000", int32(0), int32(100000)},
		{"9223372036854775807", int64(0), int64(9223372036854775807)},
		{"42", uint(0), uint(42)},
		{"255", uint8(0), uint8(255)},
		{"1000", uint16(0), uint16(1000)},
		{"100000", uint32(0), uint32(100000)},
		{"18446744073709551615", uint64(0), uint64(18446744073709551615)},
		{"3.14", float32(0), float32(3.14)},
		{"5s", time.Duration(0), 5 * time.Second},
		{"1m30s", time.Duration(0), 90 * time.Second},
		{"500ms", time.Duration(0), 500 * time.Millisecond},
		{"hello", []byte{}, []byte("hello")},
	}

	for _, tt := range tests {
		got, err := parseValue(tt.raw, tt.def)
		if err != nil {
			t.Errorf("parseValue(%q, %v) error: %v", tt.raw, tt.def, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseValue(%q, %v) = %v, want %v", tt.raw, tt.def, got, tt.want)
		}
	}
}

func TestParsePoyaTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  string
		want poyaTag
	}{
		{"", poyaTag{}},
		{"key=db_host", poyaTag{key: "db_host"}},
		{"prefix=db", poyaTag{prefix: "db"}},
		{"key=db_host,prefix=db", poyaTag{key: "db_host", prefix: "db"}},
		{"prefix=db,key=db_host", poyaTag{key: "db_host", prefix: "db"}},
	}

	for _, tt := range tests {
		got := parsePoyaTag(tt.raw)
		if got.key != tt.want.key || got.prefix != tt.want.prefix {
			t.Errorf("parsePoyaTag(%q) = {key:%q prefix:%q}, want {key:%q prefix:%q}",
				tt.raw, got.key, got.prefix, tt.want.key, tt.want.prefix)
		}
	}
}

func TestSDKWithNoPrefix(t *testing.T) {
	t.Parallel()
	p := newMockProvider()
	sdk := New(Config{Provider: p})

	val := NewDcValue("default")
	Register(sdk, "mykey", val)

	sdk.mu.RLock()
	defer sdk.mu.RUnlock()

	if _, ok := sdk.values["mykey"]; !ok {
		t.Error("key without prefix should be registered as-is")
	}
}

func TestSDKStopCancelsWatchers(t *testing.T) {
	t.Parallel()
	p := newMockProvider()
	watchStopped := make(chan struct{}, 1)

	p.watchFn = func(ctx context.Context, _ []string, _ func(_ string, _ string)) error {
		<-ctx.Done()
		watchStopped <- struct{}{}
		return nil
	}

	sdk := New(Config{Provider: p})
	val := NewDcValue("default")
	Register(sdk, "key", val)

	sdk.Start()
	sdk.Stop()

	select {
	case <-watchStopped:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher was not cancelled after Stop()")
	}
}

func TestNoopMetrics(t *testing.T) {
	t.Parallel()
	m := metrics.NoopMetrics{}
	m.IncWatchEvents("key")
	m.IncWatchErrors("key")
	m.ObserveUpdateLatency("key", time.Second)
	m.SetRegisteredKeys(5)
}

type fakeMetrics struct {
	mu            sync.Mutex
	watchEvents   int
	watchErrors   int
	updateLatency time.Duration
}

func (f *fakeMetrics) IncWatchEvents(_ string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watchEvents++
}

func (f *fakeMetrics) IncWatchErrors(_ string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watchErrors++
}

func (f *fakeMetrics) ObserveUpdateLatency(_ string, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateLatency += d
}

func (f *fakeMetrics) SetRegisteredKeys(_ int) {}

var _ metrics.Metrics = (*fakeMetrics)(nil)
var _ provider.Provider = (*mockProvider)(nil)
