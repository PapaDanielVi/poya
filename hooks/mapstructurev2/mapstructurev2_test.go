package mapstructurev2_test

import (
	"net"
	"testing"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/hooks/mapstructurev2"
	"github.com/go-viper/mapstructure/v2"
)

type testConfig struct {
	Host    *poya.DcValue[string] `mapstructure:"host"`
	Port    *poya.DcValue[int]    `mapstructure:"port"`
	Verbose *poya.DcValue[bool]   `mapstructure:"verbose"`
}

type dbConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func decode(t *testing.T, input any, target any) {
	t.Helper()
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructurev2.HookFunc(),
		Result:     target,
	})
	if err != nil {
		t.Fatalf("NewDecoder error: %v", err)
	}
	if err = decoder.Decode(input); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
}

func TestHookFunc_Scalars(t *testing.T) {
	t.Parallel()
	var cfg testConfig
	decode(t, map[string]any{
		"host":    "api.example.com",
		"port":    float64(443),
		"verbose": true,
	}, &cfg)

	if got := cfg.Host.Get(); got != "api.example.com" {
		t.Fatalf("Host.Get() = %q, want api.example.com", got)
	}
	if got := cfg.Port.Get(); got != 443 {
		t.Fatalf("Port.Get() = %d, want 443", got)
	}
	if got := cfg.Verbose.Get(); !got {
		t.Fatalf("Verbose.Get() = %v, want true", got)
	}
}

func TestHookFunc_ScalarFromString(t *testing.T) {
	t.Parallel()
	// Simulates env/flat config where every value arrives as a string.
	var cfg testConfig
	decode(t, map[string]any{
		"host":    "api.example.com",
		"port":    "8443",
		"verbose": "true",
	}, &cfg)

	if got := cfg.Port.Get(); got != 8443 {
		t.Fatalf("Port.Get() = %d, want 8443", got)
	}
	if got := cfg.Verbose.Get(); !got {
		t.Fatalf("Verbose.Get() = %v, want true", got)
	}
}

func TestHookFunc_StructT(t *testing.T) {
	t.Parallel()
	type cfgT struct {
		DB *poya.DcValue[dbConfig] `mapstructure:"db"`
	}
	var cfg cfgT
	decode(t, map[string]any{
		"db": map[string]any{"host": "localhost", "port": float64(5432)},
	}, &cfg)

	got := cfg.DB.Get()
	if got.Host != "localhost" || got.Port != 5432 {
		t.Fatalf("DB.Get() = %+v, want {localhost 5432}", got)
	}
	if cfg.DB.InternalKind() != poya.EntryKindStruct {
		t.Fatalf("DB.InternalKind() = %d, want struct", cfg.DB.InternalKind())
	}
}

func TestHookFunc_SliceT(t *testing.T) {
	t.Parallel()
	type cfgT struct {
		Origins *poya.DcValue[[]string] `mapstructure:"origins"`
	}
	var cfg cfgT
	decode(t, map[string]any{
		"origins": []any{"https://a.com", "https://b.com"},
	}, &cfg)

	got := cfg.Origins.Get()
	if len(got) != 2 || got[0] != "https://a.com" || got[1] != "https://b.com" {
		t.Fatalf("Origins.Get() = %v, want [https://a.com https://b.com]", got)
	}
	if cfg.Origins.InternalKind() != poya.EntryKindArray {
		t.Fatalf("Origins.InternalKind() = %d, want array", cfg.Origins.InternalKind())
	}
}

func TestHookFunc_DurationFromString(t *testing.T) {
	t.Parallel()
	type cfgT struct {
		Timeout *poya.DcValue[time.Duration] `mapstructure:"timeout"`
	}
	var cfg cfgT
	decode(t, map[string]any{"timeout": "1m30s"}, &cfg)

	if got := cfg.Timeout.Get(); got != 90*time.Second {
		t.Fatalf("Timeout.Get() = %v, want 1m30s", got)
	}
}

func TestHookFunc_TimeFromRFC3339(t *testing.T) {
	t.Parallel()
	type cfgT struct {
		StartAt *poya.DcValue[time.Time] `mapstructure:"start_at"`
	}
	var cfg cfgT
	decode(t, map[string]any{"start_at": "2026-06-19T10:30:00Z"}, &cfg)

	want, _ := time.Parse(time.RFC3339, "2026-06-19T10:30:00Z")
	if got := cfg.StartAt.Get(); !got.Equal(want) {
		t.Fatalf("StartAt.Get() = %v, want %v", got, want)
	}
}

// TestHookFunc_TextUnmarshaler verifies a named scalar whose pointer implements
// encoding.TextUnmarshaler (here net.IP) parses from its string form.
func TestHookFunc_TextUnmarshaler(t *testing.T) {
	t.Parallel()
	type cfgT struct {
		Bind *poya.DcValue[net.IP] `mapstructure:"bind"`
	}
	var cfg cfgT
	decode(t, map[string]any{"bind": "10.0.0.1"}, &cfg)

	if got := cfg.Bind.Get(); !got.Equal(net.ParseIP("10.0.0.1")) {
		t.Fatalf("Bind.Get() = %v, want 10.0.0.1", got)
	}
}

func TestHookFunc_PreInitializedTarget(t *testing.T) {
	t.Parallel()
	cfg := testConfig{Host: poya.NewDcValue("default-host")}
	originalPtr := cfg.Host
	decode(t, map[string]any{"host": "new-host"}, &cfg)

	if cfg.Host != originalPtr {
		t.Fatal("Host pointer changed; expected in-place update")
	}
	if got := cfg.Host.Get(); got != "new-host" {
		t.Fatalf("Host.Get() = %q, want new-host", got)
	}
}

func TestHookFunc_PassThrough(t *testing.T) {
	t.Parallel()
	type mixed struct {
		Name    string             `mapstructure:"name"`
		Timeout *poya.DcValue[int] `mapstructure:"timeout"`
	}
	var cfg mixed
	decode(t, map[string]any{"name": "myapp", "timeout": float64(30)}, &cfg)

	if cfg.Name != "myapp" {
		t.Fatalf("Name = %q, want myapp", cfg.Name)
	}
	if got := cfg.Timeout.Get(); got != 30 {
		t.Fatalf("Timeout.Get() = %d, want 30", got)
	}
}

func TestStringToDcValueHookFunc_Standalone(t *testing.T) {
	t.Parallel()
	var cfg testConfig
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructurev2.StringToDcValueHookFunc(),
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
	var cfg cfgT
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructurev2.JSONStringHookFunc(),
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

func TestHookFuncValue(t *testing.T) {
	t.Parallel()
	var cfg testConfig
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructurev2.HookFuncValue(),
		Result:     &cfg,
	})
	if err != nil {
		t.Fatalf("NewDecoder error: %v", err)
	}
	if err := decoder.Decode(map[string]any{"host": "value-test"}); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if got := cfg.Host.Get(); got != "value-test" {
		t.Fatalf("Host.Get() = %q, want value-test", got)
	}
}
