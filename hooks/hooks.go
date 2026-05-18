// Package hooks provides mapstructure decode hooks for use with DcValue[T]
// fields. When decoding YAML or map[string]interface{} data into structs
// containing DcValue[T] fields, use MapstructureHookFunc() so that
// mapstructure automatically constructs properly-typed DcValue instances.
package hooks

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/PapaDanielVi/poya"
	"github.com/mitchellh/mapstructure"
)

// MapstructureHookFunc returns a mapstructure.DecodeHookFunc that detects
// target fields of type *poya.DcValue[T] and initializes them from the
// decoded source data.
//
// For scalar T types (string, int, bool, etc.), the source value is set
// directly. For struct T types, the source value (typically
// map[string]interface{} from YAML decoding) is JSON-marshaled and then
// set via InternalSetJSON.
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
	return mapstructure.DecodeHookFuncValue(func(from reflect.Value, to reflect.Value) (interface{}, error) {
		return hookFunc(from, to)
	})
}

// MapstructureHookFuncValue returns a mapstructure.DecodeHookFuncValue for
// use with ComposeDecodeHookFunc or when the value hook type is needed.
func MapstructureHookFuncValue() mapstructure.DecodeHookFuncValue {
	return hookFunc
}

func hookFunc(from reflect.Value, to reflect.Value) (interface{}, error) {
	toType := to.Type()

	// mapstructure may call us with *DcValue[T] (pointer) or DcValue[T]
	// (struct) depending on the decode path. We handle both.
	elemType := toType
	if toType.Kind() == reflect.Pointer {
		elemType = toType.Elem()
	}

	if elemType.Kind() != reflect.Struct {
		return from.Interface(), nil
	}
	if elemType.PkgPath() != "github.com/PapaDanielVi/poya" {
		return from.Interface(), nil
	}
	typeName := elemType.Name()
	if idx := strings.IndexByte(typeName, '['); idx >= 0 {
		typeName = typeName[:idx]
	}
	if typeName != "DcValue" {
		return from.Interface(), nil
	}

	// Extract T from the defaultValue field (index 1).
	// DcValue[T] fields: [0]=key string, [1]=defaultValue T, [2]=val atomic.Value, [3]=kind entryKind.
	if elemType.NumField() < 4 {
		return from.Interface(), nil
	}
	tType := elemType.Field(1).Type

	// Struct T with map source (common when YAML decodes nested objects).
	if tType.Kind() == reflect.Struct && from.Kind() == reflect.Map {
		return handleStructDcValue(elemType, tType, from.Interface())
	}

	// Scalar (or other directly-convertible) T.
	if !from.Type().ConvertibleTo(tType) {
		return from.Interface(), nil
	}
	converted := from.Convert(tType)

	// If the target is a non-nil *DcValue[T], update in place.
	if toType.Kind() == reflect.Pointer && !to.IsNil() {
		callSetDefaultAndValue(to, converted)
		return to.Interface(), nil
	}

	// Allocate a new *DcValue[T] and initialize it.
	result := reflect.New(elemType)
	callSetDefaultAndValue(result, converted)
	return result.Interface(), nil
}

func handleStructDcValue(elemType, tType reflect.Type, data interface{}) (interface{}, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("hooks: failed to marshal struct data for %s: %w", elemType, err)
	}

	result := reflect.New(elemType)

	// Set kind to entryKindStruct by storing a zero T via SetDefaultAndValue.
	zeroVal := reflect.Zero(tType)
	callSetDefaultAndValue(result, zeroVal)

	// Now set the actual struct value via InternalSetJSON.
	setJSONMethod := result.MethodByName("InternalSetJSON")
	if !setJSONMethod.IsValid() {
		return nil, fmt.Errorf("hooks: InternalSetJSON method not found on %s", elemType)
	}
	errs := setJSONMethod.Call([]reflect.Value{reflect.ValueOf(jsonBytes)})
	if !errs[0].IsNil() {
		return nil, fmt.Errorf("hooks: InternalSetJSON failed: %w", errs[0].Interface().(error))
	}

	return result.Interface(), nil
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
