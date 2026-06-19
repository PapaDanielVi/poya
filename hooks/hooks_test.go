package hooks_test

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/hooks"
	"github.com/PapaDanielVi/poya/provider"
	"github.com/mitchellh/mapstructure"
)

// minimal mock provider for integration tests.
type integMockProvider struct {
	mu     sync.Mutex
	values map[string]string
}

func newIntegMockProvider() *integMockProvider {
	return &integMockProvider{values: make(map[string]string)}
}

func (m *integMockProvider) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[key], nil
}

func (m *integMockProvider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
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

func (m *integMockProvider) Close() error { return nil }

var _ provider.Provider = (*integMockProvider)(nil)

type testConfig struct {
	Host    *poya.DcValue[string] `mapstructure:"host"`
	Port    *poya.DcValue[int]    `mapstructure:"port"`
	Verbose *poya.DcValue[bool]   `mapstructure:"verbose"`
}

type dbConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type testStructConfig struct {
	DB *poya.DcValue[dbConfig] `mapstructure:"db"`
}

type mixedConfig struct {
	Name    string             `mapstructure:"name"`
	Timeout *poya.DcValue[int] `mapstructure:"timeout"`
}

func decodeHook(t *testing.T, input any, target any) {
	t.Helper()
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: hooks.MapstructureHookFunc(),
		Result:     target,
	})
	if err != nil {
		t.Fatalf("NewDecoder error: %v", err)
	}
	if err = decoder.Decode(input); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
}

func TestMapstructureHookFunc_ScalarString(t *testing.T) {
	t.Parallel()
	var cfg testConfig
	decodeHook(t, map[string]any{"host": "example.com"}, &cfg)

	if cfg.Host == nil {
		t.Fatal("Host is nil")
	}
	if got := cfg.Host.Get(); got != "example.com" {
		t.Fatalf("Host.Get() = %q, want %q", got, "example.com")
	}
}

func TestMapstructureHookFunc_ScalarIntFromFloat64(t *testing.T) {
	t.Parallel()
	var cfg testConfig
	decodeHook(t, map[string]any{"port": float64(8080)}, &cfg)

	if cfg.Port == nil {
		t.Fatal("Port is nil")
	}
	if got := cfg.Port.Get(); got != 8080 {
		t.Fatalf("Port.Get() = %d, want %d", got, 8080)
	}
}

func TestMapstructureHookFunc_ScalarBool(t *testing.T) {
	t.Parallel()
	var cfg testConfig
	decodeHook(t, map[string]any{"verbose": true}, &cfg)

	if cfg.Verbose == nil {
		t.Fatal("Verbose is nil")
	}
	if got := cfg.Verbose.Get(); got != true {
		t.Fatalf("Verbose.Get() = %v, want true", got)
	}
}

func TestMapstructureHookFunc_StructT(t *testing.T) {
	t.Parallel()
	var cfg testStructConfig
	decodeHook(t, map[string]any{
		"db": map[string]any{
			"host": "localhost",
			"port": float64(5432),
		},
	}, &cfg)

	if cfg.DB == nil {
		t.Fatal("DB is nil")
	}
	got := cfg.DB.Get()
	if got.Host != "localhost" || got.Port != 5432 {
		t.Fatalf("DB.Get() = %+v, want {localhost 5432}", got)
	}
}

func TestMapstructureHookFunc_PreInitializedTarget(t *testing.T) {
	t.Parallel()
	cfg := testConfig{
		Host: poya.NewDcValue("default-host"),
	}
	originalPtr := cfg.Host
	decodeHook(t, map[string]any{"host": "new-host"}, &cfg)

	if cfg.Host != originalPtr {
		t.Fatal("Host pointer changed; expected in-place update")
	}
	if got := cfg.Host.Get(); got != "new-host" {
		t.Fatalf("Host.Get() = %q, want %q", got, "new-host")
	}
}

func TestMapstructureHookFunc_NilTarget(t *testing.T) {
	t.Parallel()
	var cfg testConfig
	decodeHook(t, map[string]any{"host": "from-yaml"}, &cfg)

	if cfg.Host == nil {
		t.Fatal("Host is nil; expected allocation")
	}
	if got := cfg.Host.Get(); got != "from-yaml" {
		t.Fatalf("Host.Get() = %q, want %q", got, "from-yaml")
	}
}

func TestMapstructureHookFunc_PassThrough(t *testing.T) {
	t.Parallel()
	var cfg mixedConfig
	decodeHook(t, map[string]any{
		"name":    "myapp",
		"timeout": float64(30),
	}, &cfg)

	if cfg.Name != "myapp" {
		t.Fatalf("Name = %q, want %q", cfg.Name, "myapp")
	}
	if cfg.Timeout == nil {
		t.Fatal("Timeout is nil")
	}
	if got := cfg.Timeout.Get(); got != 30 {
		t.Fatalf("Timeout.Get() = %d, want %d", got, 30)
	}
}

func TestMapstructureHookFunc_MissingKey(t *testing.T) {
	t.Parallel()
	var cfg testConfig
	decodeHook(t, map[string]any{}, &cfg)

	if cfg.Host != nil {
		t.Fatal("Host should be nil when key is missing from input")
	}
}

func TestMapstructureHookFunc_KindDetection(t *testing.T) {
	t.Parallel()
	var cfg testStructConfig
	decodeHook(t, map[string]any{
		"db": map[string]any{
			"host": "localhost",
			"port": float64(5432),
		},
	}, &cfg)

	if cfg.DB.InternalKind() != 1 { // entryKindStruct == 1
		t.Fatalf("DB.InternalKind() = %d, want 1 (entryKindStruct)", cfg.DB.InternalKind())
	}

	var cfg2 testConfig
	decodeHook(t, map[string]any{"host": "example.com"}, &cfg2)
	if cfg2.Host.InternalKind() != 0 { // entryKindScalar == 0
		t.Fatalf("Host.InternalKind() = %d, want 0 (entryKindScalar)", cfg2.Host.InternalKind())
	}
}

func TestMapstructureHookFunc_ComposeWithWeaklyTyped(t *testing.T) {
	t.Parallel()
	hook := mapstructure.ComposeDecodeHookFunc(
		hooks.MapstructureHookFunc(),
		mapstructure.StringToTimeDurationHookFunc(),
	)

	var cfg testConfig
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: hook,
		Result:     &cfg,
	})
	if err != nil {
		t.Fatalf("NewDecoder error: %v", err)
	}
	if err = decoder.Decode(map[string]any{"port": float64(42)}); err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if cfg.Port == nil {
		t.Fatal("Port is nil")
	}
	if got := cfg.Port.Get(); got != 42 {
		t.Fatalf("Port.Get() = %d, want %d", got, 42)
	}
}

func TestMapstructureHookFunc_HookFuncValue(t *testing.T) {
	t.Parallel()
	hook := hooks.MapstructureHookFuncValue()

	var cfg testConfig
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: hook,
		Result:     &cfg,
	})
	if err != nil {
		t.Fatalf("NewDecoder error: %v", err)
	}
	if err := decoder.Decode(map[string]any{"host": "value-test"}); err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if got := cfg.Host.Get(); got != "value-test" {
		t.Fatalf("Host.Get() = %q, want %q", got, "value-test")
	}
}

func TestMapstructureHookFunc_StructDefaultValue(t *testing.T) {
	t.Parallel()
	cfg := testStructConfig{
		DB: poya.NewDcValue(dbConfig{Host: "default", Port: 3306}),
	}
	decodeHook(t, map[string]any{
		"db": map[string]any{
			"host": "override",
			"port": float64(5432),
		},
	}, &cfg)

	got := cfg.DB.Get()
	if got.Host != "override" || got.Port != 5432 {
		t.Fatalf("DB.Get() = %+v, want {override 5432}", got)
	}
}

func TestMapstructureHookFunc_AllScalarFields(t *testing.T) {
	t.Parallel()
	var cfg testConfig
	decodeHook(t, map[string]any{
		"host":    "api.example.com",
		"port":    float64(443),
		"verbose": true,
	}, &cfg)

	if got := cfg.Host.Get(); got != "api.example.com" {
		t.Fatalf("Host.Get() = %q, want %q", got, "api.example.com")
	}
	if got := cfg.Port.Get(); got != 443 {
		t.Fatalf("Port.Get() = %d, want %d", got, 443)
	}
	if got := cfg.Verbose.Get(); got != true {
		t.Fatalf("Verbose.Get() = %v, want true", got)
	}
}

func TestMapstructureHookFunc_ReturnType(t *testing.T) {
	t.Parallel()
	hook := hooks.MapstructureHookFunc()

	// Verify the underlying func type is correct.
	rv := reflect.ValueOf(hook)
	if rv.Kind() != reflect.Func {
		t.Fatalf("MapstructureHookFunc() returned %v, want func", rv.Kind())
	}
}

func TestHookThenRegisterConfig_EndToEnd(t *testing.T) {
	t.Parallel()
	type AppConfig struct {
		Host    *poya.DcValue[string] `mapstructure:"host" poya:"key=db_host"`
		Port    *poya.DcValue[int]    `mapstructure:"port" poya:"key=db_port"`
		Verbose *poya.DcValue[bool]   `mapstructure:"verbose" poya:"key=verbose"`
	}

	// Step 1: Decode YAML-like map into struct using the hook.
	var cfg AppConfig
	decodeHook(t, map[string]any{
		"host":    "db.example.com",
		"port":    float64(3306),
		"verbose": true,
	}, &cfg)

	// Step 2: Verify DcValues hold the decoded defaults.
	if got := cfg.Host.Get(); got != "db.example.com" {
		t.Fatalf("Host.Get() = %q, want %q", got, "db.example.com")
	}
	if got := cfg.Port.Get(); got != 3306 {
		t.Fatalf("Port.Get() = %d, want %d", got, 3306)
	}
	if got := cfg.Verbose.Get(); got != true {
		t.Fatalf("Verbose.Get() = %v, want true", got)
	}

	// Step 3: Register with the SDK and a provider that has different values.
	var wg sync.WaitGroup
	wg.Add(3)
	blockingMock := &blockingMockProvider{
		values: map[string]string{
			"myapp/db_host": "provider-host",
			"myapp/db_port": "5432",
			"myapp/verbose": "false",
		},
		wg: &wg,
	}

	sdk := poya.New(poya.Config{Provider: blockingMock, Prefix: "myapp/"})
	poya.RegisterConfig(sdk, &cfg)
	sdk.Start()

	// Wait for all three Watch callbacks to fire.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for provider Watch callbacks")
	}

	// Step 4: Provider values should have overridden the decoded defaults.
	if got := cfg.Host.Get(); got != "provider-host" {
		t.Fatalf("Host.Get() after provider = %q, want %q", got, "provider-host")
	}
	if got := cfg.Port.Get(); got != 5432 {
		t.Fatalf("Port.Get() after provider = %d, want %d", got, 5432)
	}
	if got := cfg.Verbose.Get(); got != false {
		t.Fatalf("Verbose.Get() after provider = %v, want false", got)
	}

	sdk.Stop()
}

// blockingMockProvider calls Done on a WaitGroup after each Watch callback,
// so tests can wait for all callbacks to complete.
type blockingMockProvider struct {
	mu     sync.Mutex
	values map[string]string
	wg     *sync.WaitGroup
}

func (m *blockingMockProvider) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[key], nil
}

func (m *blockingMockProvider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
	m.mu.Lock()
	for _, key := range keys {
		val := m.values[key]
		if val != "" {
			onChange(key, val)
		}
		if m.wg != nil {
			m.wg.Done()
		}
	}
	m.mu.Unlock()
	<-ctx.Done()
	return nil
}

func (m *blockingMockProvider) Close() error { return nil }

func TestMapstructureHookFunc_SliceT(t *testing.T) {
	t.Parallel()
	type cfgT struct {
		Origins *poya.DcValue[[]string] `mapstructure:"origins"`
	}
	var cfg cfgT
	decodeHook(t, map[string]any{
		"origins": []any{"https://a.com", "https://b.com"},
	}, &cfg)

	if cfg.Origins == nil {
		t.Fatal("Origins is nil")
	}
	got := cfg.Origins.Get()
	if len(got) != 2 || got[0] != "https://a.com" || got[1] != "https://b.com" {
		t.Fatalf("Origins.Get() = %v, want [https://a.com https://b.com]", got)
	}
	if cfg.Origins.InternalKind() != 2 { // EntryKindArray == 2
		t.Fatalf("Origins.InternalKind() = %d, want 2 (array)", cfg.Origins.InternalKind())
	}
}

func TestMapstructureHookFunc_DurationFromString(t *testing.T) {
	t.Parallel()
	type cfgT struct {
		Timeout *poya.DcValue[time.Duration] `mapstructure:"timeout"`
	}
	var cfg cfgT
	decodeHook(t, map[string]any{"timeout": "1m30s"}, &cfg)

	if cfg.Timeout == nil {
		t.Fatal("Timeout is nil")
	}
	if got := cfg.Timeout.Get(); got != 90*time.Second {
		t.Fatalf("Timeout.Get() = %v, want 1m30s", got)
	}
}

func TestMapstructureHookFunc_ScalarFromString(t *testing.T) {
	t.Parallel()
	// Simulates env-style config where every value arrives as a string.
	var cfg testConfig
	decodeHook(t, map[string]any{
		"host":    "api.example.com",
		"port":    "8443",
		"verbose": "true",
	}, &cfg)

	if got := cfg.Port.Get(); got != 8443 {
		t.Fatalf("Port.Get() = %d, want 8443", got)
	}
	if got := cfg.Verbose.Get(); got != true {
		t.Fatalf("Verbose.Get() = %v, want true", got)
	}
	if got := cfg.Host.Get(); got != "api.example.com" {
		t.Fatalf("Host.Get() = %q, want api.example.com", got)
	}
}

func TestStringToDcValueHookFunc_Standalone(t *testing.T) {
	t.Parallel()
	hook := hooks.StringToDcValueHookFunc()
	var cfg testConfig
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: hook,
		Result:     &cfg,
	})
	if err != nil {
		t.Fatalf("NewDecoder error: %v", err)
	}
	if err = decoder.Decode(map[string]any{"port": "9090"}); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if cfg.Port == nil || cfg.Port.Get() != 9090 {
		t.Fatalf("Port.Get() = %v, want 9090", cfg.Port)
	}
}

func TestJSONStringHookFunc_StructAndSlice(t *testing.T) {
	t.Parallel()
	type cfgT struct {
		DB      *poya.DcValue[dbConfig] `mapstructure:"db"`
		Origins *poya.DcValue[[]string] `mapstructure:"origins"`
	}
	hook := hooks.JSONStringHookFunc()
	var cfg cfgT
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: hook,
		Result:     &cfg,
	})
	if err != nil {
		t.Fatalf("NewDecoder error: %v", err)
	}
	err = decoder.Decode(map[string]any{
		"db":      `{"host":"localhost","port":5432}`,
		"origins": `["https://a.com"]`,
	})
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if got := cfg.DB.Get(); got.Host != "localhost" || got.Port != 5432 {
		t.Fatalf("DB.Get() = %+v, want {localhost 5432}", got)
	}
	if got := cfg.Origins.Get(); len(got) != 1 || got[0] != "https://a.com" {
		t.Fatalf("Origins.Get() = %v, want [https://a.com]", got)
	}
}

func TestHookThenRegisterConfig_StructT(t *testing.T) {
	t.Parallel()
	type DBConfig struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	type AppConfig struct {
		DB *poya.DcValue[DBConfig] `mapstructure:"db" poya:"key=db_config"`
	}

	var cfg AppConfig
	decodeHook(t, map[string]any{
		"db": map[string]any{
			"host": "localhost",
			"port": float64(5432),
		},
	}, &cfg)

	if got := cfg.DB.Get(); got.Host != "localhost" || got.Port != 5432 {
		t.Fatalf("DB.Get() = %+v, want {localhost 5432}", got)
	}

	// Verify the kind was set correctly by the hook (struct vs scalar).
	if cfg.DB.InternalKind() != 1 { // entryKindStruct == 1
		t.Fatalf("DB.InternalKind() = %d, want 1 (entryKindStruct)", cfg.DB.InternalKind())
	}

	// Register with SDK and verify the decoded default survives.
	p := newIntegMockProvider()
	sdk := poya.New(poya.Config{Provider: p, Prefix: "app/"})
	poya.RegisterConfig(sdk, &cfg)

	// The DcValue should still hold the decoded default until a provider overrides it.
	if got := cfg.DB.Get(); got.Host != "localhost" || got.Port != 5432 {
		t.Fatalf("DB.Get() after register = %+v, want {localhost 5432}", got)
	}
}
