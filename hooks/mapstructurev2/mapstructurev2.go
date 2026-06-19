// Package mapstructurev2 provides decode hooks for DcValue[T] fields that target
// github.com/go-viper/mapstructure/v2, the actively maintained mapstructure fork.
//
// Several config loaders decode into structs through this fork:
//   - koanf v2 (github.com/knadh/koanf/v2): pass HookFunc() in the DecoderConfig
//     of koanf.UnmarshalConf.
//   - viper >= 1.18 (github.com/spf13/viper): pass HookFunc() via viper.DecodeHook.
//   - OpenTelemetry Collector and other tools built on go-viper/mapstructure/v2.
//
// The hooks behave identically to the sibling package
// github.com/PapaDanielVi/poya/hooks, which targets the older
// github.com/mitchellh/mapstructure (v1) used by viper < 1.18. Use whichever
// matches your loader's mapstructure dependency.
//
// koanf usage:
//
//	k := koanf.New(".")
//	// ... load providers/parsers into k ...
//	err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{
//	    Tag: "koanf",
//	    DecoderConfig: &mapstructure.DecoderConfig{
//	        DecodeHook: mapstructurev2.HookFunc(),
//	        Result:     &cfg,
//	    },
//	})
//
// viper (>= 1.18) usage:
//
//	err := viper.Unmarshal(&cfg, viper.DecodeHook(mapstructurev2.HookFunc()))
package mapstructurev2

import (
	"reflect"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/hooks/internal/dchook"
	"github.com/go-viper/mapstructure/v2"
)

// HookFunc returns a mapstructure.DecodeHookFunc that detects target fields of
// type *poya.DcValue[T] and initializes them from the decoded source data.
//
// It handles every DcValue kind:
//   - scalar T: the source value is converted, or parsed when it arrives as a
//     string (common with env vars and flat config), into T.
//   - time.Duration T: string sources like "30s" are parsed with time.ParseDuration.
//   - time.Time T: string sources in RFC3339 are parsed with time.Parse.
//   - any T whose pointer implements encoding.TextUnmarshaler: parses itself.
//   - struct T: a map source (typically from YAML) is JSON-encoded and decoded into T.
//   - slice T: a slice source is JSON-encoded and decoded into the slice.
func HookFunc() mapstructure.DecodeHookFunc {
	return mapstructure.DecodeHookFuncValue(dchook.Hook)
}

// HookFuncValue returns a mapstructure.DecodeHookFuncValue for use with
// ComposeDecodeHookFunc or when the value hook type is needed.
func HookFuncValue() mapstructure.DecodeHookFuncValue {
	return func(from, to reflect.Value) (any, error) {
		return dchook.Hook(from, to)
	}
}

// StringToDcValueHookFunc returns a hook that only handles string sources
// targeting a DcValue[T] with a non-string scalar T (int, uint, float, bool,
// complex, time.Duration, time.Time). It is the composable piece used when
// configuration arrives entirely as strings, such as from environment
// variables. Non-matching values pass through unchanged so it can be combined
// with other hooks via mapstructure.ComposeDecodeHookFunc.
func StringToDcValueHookFunc() mapstructure.DecodeHookFunc {
	return mapstructure.DecodeHookFuncValue(dchook.StringToDcValue)
}

// JSONStringHookFunc returns a hook that decodes a JSON string source (for
// example a struct or array stored as a single config value) into a struct or
// slice DcValue[T]. Non-matching values pass through unchanged.
func JSONStringHookFunc() mapstructure.DecodeHookFunc {
	return mapstructure.DecodeHookFuncValue(dchook.JSONString)
}

// ensure poya is used (imported for side effects).
var _ *poya.DcValue[string]
