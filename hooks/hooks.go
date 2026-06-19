// Package hooks provides mapstructure decode hooks for use with DcValue[T]
// fields. When decoding YAML, env, or map[string]any data into structs
// containing DcValue[T] fields, use MapstructureHookFunc() so that
// mapstructure automatically constructs properly-typed DcValue instances.
package hooks

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/mitchellh/mapstructure"
)

const (
	poyaPkgPath = "github.com/PapaDanielVi/poya"
	dcValueName = "DcValue"
	// dcValueFieldCount is the number of fields on DcValue[T]:
	// key, defaultValue, val, kind.
	dcValueFieldCount = 4
	// dcValueDefaultField is the index of the defaultValue field, whose type is T.
	dcValueDefaultField = 1
)

var durationType = reflect.TypeFor[time.Duration]()

// MapstructureHookFunc returns a mapstructure.DecodeHookFunc that detects
// target fields of type *poya.DcValue[T] and initializes them from the
// decoded source data.
//
// It handles every DcValue kind:
//   - scalar T: the source value is converted, or parsed when it arrives as a
//     string (common with env vars and flat config), into T.
//   - time.Duration T: string sources like "30s" are parsed with time.ParseDuration.
//   - struct T: a map source (typically from YAML) is JSON-encoded and decoded into T.
//   - slice T: a slice source is JSON-encoded and decoded into the slice.
//
// Usage:
//
//	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
//	    DecodeHook: hooks.MapstructureHookFunc(),
//	    Result:     &cfg,
//	})
//	if err != nil { ... }
//	err = decoder.Decode(viper.AllSettings())
func MapstructureHookFunc() mapstructure.DecodeHookFunc {
	return mapstructure.DecodeHookFuncValue(hookFunc)
}

// MapstructureHookFuncValue returns a mapstructure.DecodeHookFuncValue for
// use with ComposeDecodeHookFunc or when the value hook type is needed.
func MapstructureHookFuncValue() mapstructure.DecodeHookFuncValue {
	return hookFunc
}

// StringToDcValueHookFunc returns a hook that only handles string sources
// targeting a DcValue[T] with a non-string scalar T (int, uint, float, bool,
// time.Duration). It is the composable piece used when configuration arrives
// entirely as strings, such as from environment variables. Non-matching values
// pass through unchanged so it can be combined with other hooks via
// mapstructure.ComposeDecodeHookFunc.
func StringToDcValueHookFunc() mapstructure.DecodeHookFunc {
	return mapstructure.DecodeHookFuncValue(func(from, to reflect.Value) (any, error) {
		if from.Kind() != reflect.String {
			return from.Interface(), nil
		}
		elemType, tType, ok := dcValueTypes(to.Type())
		if !ok || tType.Kind() == reflect.String {
			return from.Interface(), nil
		}
		parsed, ok := parseScalarString(from.String(), tType)
		if !ok {
			return from.Interface(), nil
		}
		return setScalar(to, elemType, parsed), nil
	})
}

// JSONStringHookFunc returns a hook that decodes a JSON string source (for
// example a struct or array stored as a single config value) into a struct or
// slice DcValue[T]. Non-matching values pass through unchanged.
func JSONStringHookFunc() mapstructure.DecodeHookFunc {
	return mapstructure.DecodeHookFuncValue(func(from, to reflect.Value) (any, error) {
		if from.Kind() != reflect.String {
			return from.Interface(), nil
		}
		elemType, tType, ok := dcValueTypes(to.Type())
		if !ok || (tType.Kind() != reflect.Struct && tType.Kind() != reflect.Slice) {
			return from.Interface(), nil
		}
		var data any
		if err := json.Unmarshal([]byte(from.String()), &data); err != nil {
			return from.Interface(), nil
		}
		return handleJSONDcValue(elemType, tType, data)
	})
}

// dcValueTypes reports whether toType targets a poya.DcValue[T]. When it does,
// it returns the DcValue struct type, the element type T, and true.
func dcValueTypes(toType reflect.Type) (elemType, tType reflect.Type, ok bool) {
	elemType = toType
	if toType.Kind() == reflect.Pointer {
		elemType = toType.Elem()
	}
	if elemType.Kind() != reflect.Struct || elemType.PkgPath() != poyaPkgPath {
		return nil, nil, false
	}
	typeName := elemType.Name()
	if idx := strings.IndexByte(typeName, '['); idx >= 0 {
		typeName = typeName[:idx]
	}
	if typeName != dcValueName || elemType.NumField() < dcValueFieldCount {
		return nil, nil, false
	}
	return elemType, elemType.Field(dcValueDefaultField).Type, true
}

func hookFunc(from reflect.Value, to reflect.Value) (any, error) {
	elemType, tType, ok := dcValueTypes(to.Type())
	if !ok {
		return from.Interface(), nil
	}

	// Struct T with a map source (common when YAML decodes nested objects).
	if tType.Kind() == reflect.Struct && from.Kind() == reflect.Map {
		return handleJSONDcValue(elemType, tType, from.Interface())
	}

	// Slice T with a slice source (e.g. []any from YAML into []string).
	if tType.Kind() == reflect.Slice && from.Kind() == reflect.Slice {
		return handleJSONDcValue(elemType, tType, from.Interface())
	}

	// String source for a non-string scalar T: parse it (env vars, flat config).
	if from.Kind() == reflect.String && tType.Kind() != reflect.String {
		if parsed, parsedOK := parseScalarString(from.String(), tType); parsedOK {
			return setScalar(to, elemType, parsed), nil
		}
		return from.Interface(), nil
	}

	// Directly convertible scalar T.
	if !from.Type().ConvertibleTo(tType) {
		return from.Interface(), nil
	}
	return setScalar(to, elemType, from.Convert(tType)), nil
}

// parseScalarString parses raw into a reflect.Value of type tType for the
// scalar kinds poya supports. The boolean reports whether parsing succeeded.
func parseScalarString(raw string, tType reflect.Type) (reflect.Value, bool) {
	if tType == durationType {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(d).Convert(tType), true
	}
	//nolint:exhaustive // only the scalar kinds poya parses from strings are handled.
	switch tType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(i).Convert(tType), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(u).Convert(tType), true
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(f).Convert(tType), true
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(b), true
	default:
		return reflect.Value{}, false
	}
}

func handleJSONDcValue(elemType, tType reflect.Type, data any) (any, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("hooks: failed to marshal data for %s: %w", elemType, err)
	}

	result := reflect.New(elemType)

	// Set the kind (struct or array) by storing a zero T via SetDefaultAndValue.
	zeroVal := reflect.Zero(tType)
	callSetDefaultAndValue(result, zeroVal)

	// Now set the actual value via InternalSetJSON.
	setJSONMethod := result.MethodByName("InternalSetJSON")
	if !setJSONMethod.IsValid() {
		return nil, fmt.Errorf("hooks: InternalSetJSON method not found on %s", elemType)
	}
	errs := setJSONMethod.Call([]reflect.Value{reflect.ValueOf(jsonBytes)})
	if !errs[0].IsNil() {
		if jsonErr, ok := errs[0].Interface().(error); ok {
			return nil, fmt.Errorf("hooks: InternalSetJSON failed: %w", jsonErr)
		}
		return nil, fmt.Errorf("hooks: InternalSetJSON failed: %v", errs[0].Interface())
	}

	return result.Interface(), nil
}

// setScalar stores converted into a DcValue[T]. When the target is an existing
// non-nil *DcValue[T] it updates in place; otherwise it allocates a new one.
func setScalar(to reflect.Value, elemType reflect.Type, converted reflect.Value) any {
	if to.Kind() == reflect.Pointer && !to.IsNil() {
		callSetDefaultAndValue(to, converted)
		return to.Interface()
	}
	result := reflect.New(elemType)
	callSetDefaultAndValue(result, converted)
	return result.Interface()
}

func callSetDefaultAndValue(target reflect.Value, val reflect.Value) {
	method := target.MethodByName("SetDefaultAndValue")
	if !method.IsValid() {
		return
	}
	method.Call([]reflect.Value{val})
}

// ensure poya is used (imported for side effects).
var _ *poya.DcValue[string]
