package gxsql

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// jsonExactIntegerMax is the largest integer JSON numbers represent exactly in
// IEEE-754 double precision (2^53 - 1).
const jsonExactIntegerMax int64 = 9007199254740991

// NormalizedValue is a tagged, JSON-safe encoding of a SQL-returned scalar,
// bound argument, or diagnostic value. Integer magnitudes outside the exact
// JSON integer range use integer_string. Finite integral float64 values within
// the exact JSON integer range encode as json_integer with Exact true;
// non-integral floats use json_number, preserving signed zero as -0.0; other
// lossy encodings such as invalid UTF-8 strings (replaced with U+FFFD) and
// unsupported types (structural type name only) set Exact false.
type NormalizedValue struct {
	// Kind identifies the encoding. One of: null, bool, string, json_integer,
	// json_number, integer_string, decimal_string, bytes_base64, time_rfc3339,
	// composite, non_finite, unsupported.
	Kind string `json:"kind"`
	// Value holds the encoded payload. Omitted when Kind is null.
	Value any `json:"value,omitempty"`
	// Exact reports lossless encoding. Omitted when false.
	Exact bool `json:"exact,omitempty"`
}

// jsonNumber preserves signed-zero wire encoding as -0.0 per v1 export contract.
type jsonNumber float64

func (n jsonNumber) MarshalJSON() ([]byte, error) {
	f := float64(n)
	if f == 0 && math.Signbit(f) {
		return []byte("-0.0"), nil
	}
	return json.Marshal(f)
}

// normalizeValue converts v into a NormalizedValue suitable for JSON export.
func normalizeValue(v any) (NormalizedValue, error) {
	if v == nil {
		return NormalizedValue{Kind: "null", Exact: true}, nil
	}

	switch x := v.(type) {
	case bool:
		return NormalizedValue{Kind: "bool", Value: x, Exact: true}, nil
	case string:
		if !utf8.ValidString(x) {
			return NormalizedValue{
				Kind:  "string",
				Value: strings.ToValidUTF8(x, "\uFFFD"),
				Exact: false,
			}, nil
		}
		return NormalizedValue{Kind: "string", Value: x, Exact: true}, nil
	case []byte:
		return NormalizedValue{
			Kind:  "bytes_base64",
			Value: base64.StdEncoding.EncodeToString(x),
			Exact: true,
		}, nil
	case time.Time:
		return NormalizedValue{
			Kind:  "time_rfc3339",
			Value: x.UTC().Format(time.RFC3339Nano),
			Exact: true,
		}, nil
	case float32:
		return normalizeFloat(float64(x))
	case float64:
		return normalizeFloat(x)
	case int:
		return normalizeInt(int64(x))
	case int8:
		return normalizeInt(int64(x))
	case int16:
		return normalizeInt(int64(x))
	case int32:
		return normalizeInt(int64(x))
	case int64:
		return normalizeInt(x)
	case uint:
		return normalizeUint(uint64(x))
	case uint8:
		return normalizeUint(uint64(x))
	case uint16:
		return normalizeUint(uint64(x))
	case uint32:
		return normalizeUint(uint64(x))
	case uint64:
		return normalizeUint(x)
	case json.Number:
		return normalizeDecimalString(string(x))
	case RowKey:
		return normalizeComposite([]any(x))
	case []any:
		return normalizeComposite(x)
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Slice:
			if rv.Type().Elem().Kind() == reflect.Uint8 {
				b := make([]byte, rv.Len())
				reflect.Copy(reflect.ValueOf(b), rv)
				return NormalizedValue{
					Kind:  "bytes_base64",
					Value: base64.StdEncoding.EncodeToString(b),
					Exact: true,
				}, nil
			}
		case reflect.Array:
			vals := make([]any, rv.Len())
			for i := range vals {
				vals[i] = rv.Index(i).Interface()
			}
			return normalizeComposite(vals)
		}
		return NormalizedValue{
			Kind:  "unsupported",
			Value: structuralTypeName(v),
			Exact: false,
		}, nil
	}
}

func normalizeComposite(vals []any) (NormalizedValue, error) {
	out := make([]NormalizedValue, len(vals))
	allExact := true
	for i, item := range vals {
		nv, err := normalizeValue(item)
		if err != nil {
			return NormalizedValue{}, err
		}
		if !nv.Exact {
			allExact = false
		}
		out[i] = nv
	}
	nv := NormalizedValue{Kind: "composite", Value: out}
	if allExact {
		nv.Exact = true
	}
	return nv, nil
}

func normalizeInt(n int64) (NormalizedValue, error) {
	if n >= -jsonExactIntegerMax && n <= jsonExactIntegerMax {
		return NormalizedValue{Kind: "json_integer", Value: n, Exact: true}, nil
	}
	return NormalizedValue{
		Kind:  "integer_string",
		Value: strconv.FormatInt(n, 10),
		Exact: true,
	}, nil
}

func normalizeUint(n uint64) (NormalizedValue, error) {
	if n <= uint64(jsonExactIntegerMax) {
		return NormalizedValue{Kind: "json_integer", Value: n, Exact: true}, nil
	}
	return NormalizedValue{
		Kind:  "integer_string",
		Value: strconv.FormatUint(n, 10),
		Exact: true,
	}, nil
}

func normalizeFloat(f float64) (NormalizedValue, error) {
	if math.IsNaN(f) {
		return NormalizedValue{Kind: "non_finite", Value: "NaN", Exact: false}, nil
	}
	if math.IsInf(f, 1) {
		return NormalizedValue{Kind: "non_finite", Value: "+Inf", Exact: false}, nil
	}
	if math.IsInf(f, -1) {
		return NormalizedValue{Kind: "non_finite", Value: "-Inf", Exact: false}, nil
	}
	if f == 0 && math.Signbit(f) {
		return NormalizedValue{Kind: "json_number", Value: jsonNumber(f), Exact: false}, nil
	}
	if isExactJSONInteger(f) {
		return NormalizedValue{Kind: "json_integer", Value: int64(f), Exact: true}, nil
	}
	return NormalizedValue{Kind: "json_number", Value: jsonNumber(f), Exact: false}, nil
}

func normalizeDecimalString(s string) (NormalizedValue, error) {
	s = strings.TrimSpace(s)
	return NormalizedValue{Kind: "decimal_string", Value: s, Exact: true}, nil
}

func isExactJSONInteger(f float64) bool {
	if f < float64(-jsonExactIntegerMax) || f > float64(jsonExactIntegerMax) {
		return false
	}
	return f == math.Trunc(f)
}

func structuralTypeName(v any) string {
	t := reflect.TypeOf(v)
	if t == nil {
		return "nil"
	}
	for t.Kind() == reflect.Pointer {
		if t.Elem() == nil {
			return "nil"
		}
		t = t.Elem()
	}
	if name := t.Name(); name != "" {
		return name
	}
	return t.Kind().String()
}

func normalizeValues(vals []any) ([]NormalizedValue, error) {
	out := make([]NormalizedValue, len(vals))
	for i, v := range vals {
		nv, err := normalizeValue(v)
		if err != nil {
			return nil, fmt.Errorf("normalize index %d: %w", i, err)
		}
		out[i] = nv
	}
	return out, nil
}
