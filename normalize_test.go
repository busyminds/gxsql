package gxsql

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
)

type barePrivate struct{}

func TestNormalizeFloatPreservesSignedZero(t *testing.T) {
	nv, err := normalizeFloat(math.Copysign(0, -1))
	if err != nil {
		t.Fatal(err)
	}
	if nv.Kind != "json_number" {
		t.Fatalf("kind = %q, want json_number", nv.Kind)
	}
	if nv.Exact {
		t.Fatal("signed zero must not be marked exact")
	}
	n, ok := nv.Value.(jsonNumber)
	if !ok {
		t.Fatalf("value type = %T, want jsonNumber", nv.Value)
	}
	f := float64(n)
	if f != 0 || !math.Signbit(f) {
		t.Fatalf("value = %#v, want signed zero", nv.Value)
	}
}

func TestNormalizeUnsupportedBarePrivateTypeUsesStructuralName(t *testing.T) {
	nv, err := normalizeValue(barePrivate{})
	if err != nil {
		t.Fatal(err)
	}
	if nv.Kind != "unsupported" {
		t.Fatalf("kind = %q, want unsupported", nv.Kind)
	}
	if nv.Value != "barePrivate" {
		t.Fatalf("value = %q, want barePrivate", nv.Value)
	}
	if nv.Exact {
		t.Fatal("unsupported values must not be exact")
	}
}

func TestNormalizeNonFiniteAndUnsupportedKinds(t *testing.T) {
	cases := []struct {
		name string
		in   any
		kind string
	}{
		{name: "nan", in: math.NaN(), kind: "non_finite"},
		{name: "pos_inf", in: math.Inf(1), kind: "non_finite"},
		{name: "neg_inf", in: math.Inf(-1), kind: "non_finite"},
		{name: "chan", in: make(chan int), kind: "unsupported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nv, err := normalizeValue(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if nv.Kind != tc.kind {
				t.Fatalf("kind = %q, want %q", nv.Kind, tc.kind)
			}
			if nv.Exact {
				t.Fatal("expected inexact encoding")
			}
		})
	}
}

func TestNormalizeSignedZeroJSONWireBytes(t *testing.T) {
	nv, err := normalizeFloat(math.Copysign(0, -1))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(nv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("-0.0")) {
		t.Fatalf("json = %s, want raw -0.0 number", data)
	}
}

func TestNormalizeInvalidUTF8StringNotExact(t *testing.T) {
	bad := string([]byte{0xff})
	nv, err := normalizeValue(bad)
	if err != nil {
		t.Fatal(err)
	}
	if nv.Kind != "string" {
		t.Fatalf("kind = %q, want string", nv.Kind)
	}
	if nv.Exact {
		t.Fatal("invalid UTF-8 must not be marked exact")
	}
	data, err := json.Marshal(nv)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatalf("invalid json: %s", data)
	}
	if !bytes.Contains(data, []byte("\ufffd")) {
		t.Fatalf("expected U+FFFD replacement in json: %s", data)
	}
}
func TestNormalizeJSONNumberDecimalString(t *testing.T) {
	nv, err := normalizeValue(json.Number("123.456"))
	if err != nil {
		t.Fatal(err)
	}
	if nv.Kind != "decimal_string" {
		t.Fatalf("kind = %q, want decimal_string", nv.Kind)
	}
	if nv.Value != "123.456" {
		t.Fatalf("value = %v", nv.Value)
	}
	if !nv.Exact {
		t.Fatal("decimal_string must be exact")
	}
}
