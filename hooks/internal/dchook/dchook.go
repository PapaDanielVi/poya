// Package dchook holds the reflection logic shared by poya's mapstructure
// decode hooks. It deliberately imports no mapstructure package so the same
// code backs both the mitchellh/mapstructure hooks (package hooks) and the
// go-viper/mapstructure/v2 hooks (package mapstructurev2). The exported
// functions all use the (from, to reflect.Value) (any, error) shape that both
// mapstructure forks accept as a DecodeHookFuncValue.
package dchook

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
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

var (
	durationType = reflect.TypeFor[time.Duration]()
	timeType     = reflect.TypeFor[time.Time]()
)

// Hook implements the general decode hook: it detects a target *poya.DcValue[T]
// and initializes it from the source, handling scalar, time.Duration, time.Time,
// struct, and slice kinds. Non-matching values pass through unchanged.
func Hook(from, to reflect.Value) (any, error) {
	elemType, tType, ok := dcValueTypes(to.Type())
	if !ok {
		return from.Interface(), nil
	}

	// Struct T with a map source (common when YAML decodes nested objects).
	if tType.Kind() == reflect.Struct && tType != timeType && from.Kind() == reflect.Map {
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

// StringToDcValue handles only string sources targeting a DcValue[T] with a
// non-string scalar T (int, uint, float, bool, complex, time.Duration,
// time.Time, or any encoding.TextUnmarshaler). Non-matching values pass through
// unchanged so it can be composed with other hooks.
func StringToDcValue(from, to reflect.Value) (any, error) {
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
}

// JSONString decodes a JSON string source (a struct or array stored as a single
// config value) into a struct or slice DcValue[T]. Non-matching values pass
// through unchanged.
func JSONString(from, to reflect.Value) (any, error) {
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

// parseScalarString parses raw into a reflect.Value of type tType for the scalar
// kinds poya supports. It mirrors poya's parseValue: time.Duration and time.Time
// are special-cased, any type whose pointer implements encoding.TextUnmarshaler
// parses itself, and the basic kinds (bool, int, uint, float, complex) are parsed
// via strconv then converted back to tType so named types round-trip. The boolean
// reports whether parsing succeeded.
func parseScalarString(raw string, tType reflect.Type) (reflect.Value, bool) {
	switch tType {
	case durationType:
		d, err := time.ParseDuration(raw)
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(d).Convert(tType), true
	case timeType:
		ts, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(ts), true
	}

	if parsed, ok := parseTextUnmarshaler(raw, tType); ok {
		return parsed, true
	}

	return parseByKind(raw, tType)
}

// parseTextUnmarshaler uses encoding.TextUnmarshaler when tType's pointer
// implements it, letting domain types parse their own string form. The boolean
// reports whether parsing succeeded.
func parseTextUnmarshaler(raw string, tType reflect.Type) (reflect.Value, bool) {
	ptr := reflect.New(tType)
	tu, ok := ptr.Interface().(encoding.TextUnmarshaler)
	if !ok {
		return reflect.Value{}, false
	}
	if err := tu.UnmarshalText([]byte(raw)); err != nil {
		return reflect.Value{}, false
	}
	return ptr.Elem(), true
}

// parseByKind parses raw into the basic kind of tType and converts the result
// back to tType so named types round-trip.
func parseByKind(raw string, tType reflect.Type) (reflect.Value, bool) {
	//nolint:exhaustive // only the scalar kinds poya parses from strings are handled.
	switch tType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(raw, 10, tType.Bits())
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(i).Convert(tType), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(raw, 10, tType.Bits())
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(u).Convert(tType), true
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, tType.Bits())
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(f).Convert(tType), true
	case reflect.Complex64, reflect.Complex128:
		c, err := strconv.ParseComplex(raw, tType.Bits())
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(c).Convert(tType), true
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(b).Convert(tType), true
	default:
		return reflect.Value{}, false
	}
}

func handleJSONDcValue(elemType, tType reflect.Type, data any) (any, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("dchook: failed to marshal data for %s: %w", elemType, err)
	}

	result := reflect.New(elemType)

	// Set the kind (struct or array) by storing a zero T via SetDefaultAndValue.
	zeroVal := reflect.Zero(tType)
	callSetDefaultAndValue(result, zeroVal)

	// Now set the actual value via InternalSetJSON.
	setJSONMethod := result.MethodByName("InternalSetJSON")
	if !setJSONMethod.IsValid() {
		return nil, fmt.Errorf("dchook: InternalSetJSON method not found on %s", elemType)
	}
	errs := setJSONMethod.Call([]reflect.Value{reflect.ValueOf(jsonBytes)})
	if !errs[0].IsNil() {
		if jsonErr, ok := errs[0].Interface().(error); ok {
			return nil, fmt.Errorf("dchook: InternalSetJSON failed: %w", jsonErr)
		}
		return nil, fmt.Errorf("dchook: InternalSetJSON failed: %v", errs[0].Interface())
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
