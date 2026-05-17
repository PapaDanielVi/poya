package poya

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/PapaDanielVi/poya/provider"
)

// mockProvider is a test double for provider.Provider.
type mockProvider struct {
	mu       sync.Mutex
	values   map[string]string
	watchFn  func(ctx context.Context, key string, onChange func(key string, value string)) error
}

func newMockProvider() *mockProvider {
	return &mockProvider{
		values: make(map[string]string),
	}
}

func (m *mockProvider) Get(ctx context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[key], nil
}

func (m *mockProvider) Watch(ctx context.Context, key string, onChange func(key string, value string)) error {
	if m.watchFn != nil {
		return m.watchFn(ctx, key, onChange)
	}
	// Default: send initial value then block until context cancelled
	m.mu.Lock()
	val := m.values[key]
	m.mu.Unlock()
	if val != "" {
		onChange(key, val)
	}
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
	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: "test/"})
	if sdk == nil {
		t.Fatal("New() returned nil")
	}
}

func TestRegisterAndGet(t *testing.T) {
	p := newMockProvider()
	p.set("test/mykey", "hello")

	sdk := New(Config{Provider: p, Prefix: "test/"})
	val := NewDcValue("default")
	Register(sdk, "mykey", val)

	if got := val.Get(); got != "default" {
		t.Errorf("Get() = %v, want %v", got, "default")
	}
}

func TestRegisterStruct(t *testing.T) {
	type AppConfig struct {
		DBHost  DcValue[string] `poya:"db_host"`
		DBPort  DcValue[int]    `poya:"db_port"`
		Verbose DcValue[bool]   `poya:"verbose"`
	}

	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: "myapp/"})

	cfg := AppConfig{
		DBHost:  *NewDcValue("localhost"),
		DBPort:  *NewDcValue(5432),
		Verbose: *NewDcValue(false),
	}

	RegisterStruct(sdk, &cfg)

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

func TestRegisterStructNested(t *testing.T) {
	type DBConfig struct {
		Host DcValue[string] `poya:"host"`
		Port DcValue[int]    `poya:"port"`
	}
	type AppConfig struct {
		DB DBConfig `poya:"db"`
	}

	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: "myapp/"})

	cfg := AppConfig{
		DB: DBConfig{
			Host: *NewDcValue("localhost"),
			Port: *NewDcValue(5432),
		},
	}

	RegisterStruct(sdk, &cfg)

	sdk.mu.RLock()
	defer sdk.mu.RUnlock()

	if _, ok := sdk.values["myapp/db/host"]; !ok {
		t.Error("nested db/host not registered")
	}
	if _, ok := sdk.values["myapp/db/port"]; !ok {
		t.Error("nested db/port not registered")
	}
}

func TestRegisterStructNoTag(t *testing.T) {
	type AppConfig struct {
		MyKey DcValue[string]
	}

	p := newMockProvider()
	sdk := New(Config{Provider: p, Prefix: ""})

	cfg := AppConfig{
		MyKey: *NewDcValue("default"),
	}

	RegisterStruct(sdk, &cfg)

	sdk.mu.RLock()
	defer sdk.mu.RUnlock()

	if _, ok := sdk.values["mykey"]; !ok {
		t.Error("field without tag should use lowercased field name")
	}
}

func TestStartUpdatesValue(t *testing.T) {
	p := newMockProvider()
	p.set("test/key", "initial")

	updateCh := make(chan struct{}, 1)
	p.watchFn = func(ctx context.Context, key string, onChange func(key string, value string)) error {
		if key == "test/key" {
			onChange(key, "initial")
			// Simulate an update after a short delay
			go func() {
				time.Sleep(50 * time.Millisecond)
				onChange(key, "updated")
				updateCh <- struct{}{}
			}()
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

	// Give the goroutine a moment to process the callback
	time.Sleep(10 * time.Millisecond)

	if got := val.Get(); got != "updated" {
		t.Errorf("Get() = %v, want %v", got, "updated")
	}

	sdk.Stop()
}

func TestParseValue(t *testing.T) {
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
	}

	for _, tt := range tests {
		got, err := parseValue(tt.raw, tt.def)
		if err != nil {
			t.Errorf("parseValue(%q, %v) error: %v", tt.raw, tt.def, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseValue(%q, %v) = %v, want %v", tt.raw, tt.def, got, tt.want)
		}
	}
}

func TestSDKWithNoPrefix(t *testing.T) {
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
	p := newMockProvider()
	watchStopped := make(chan struct{}, 1)

	p.watchFn = func(ctx context.Context, key string, onChange func(key string, value string)) error {
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
		// watcher was cancelled as expected
	case <-time.After(2 * time.Second):
		t.Fatal("watcher was not cancelled after Stop()")
	}
}

// Ensure mockProvider implements provider.Provider
var _ provider.Provider = (*mockProvider)(nil)
