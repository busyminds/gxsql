package gxsql

import (
	"encoding/json"
	"math"
	"testing"
)

func assertNormalizedValueJSONSafe(t *testing.T, nv NormalizedValue) {
	t.Helper()
	if nv.Kind == "" {
		t.Fatal("normalized value kind is empty")
	}
	if _, err := json.Marshal(nv); err != nil {
		t.Fatalf("json.Marshal(NormalizedValue): %v", err)
	}
}

func FuzzNormalizeValueString(f *testing.F) {
	f.Add("")
	f.Add("plain text")
	f.Add("\xff\xfe")
	f.Fuzz(func(t *testing.T, s string) {
		nv, err := normalizeValue(s)
		if err != nil {
			t.Fatalf("normalizeValue(string): %v", err)
		}
		assertNormalizedValueJSONSafe(t, nv)
	})
}

func FuzzNormalizeValueBytes(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	f.Fuzz(func(t *testing.T, b []byte) {
		nv, err := normalizeValue(b)
		if err != nil {
			t.Fatalf("normalizeValue([]byte): %v", err)
		}
		assertNormalizedValueJSONSafe(t, nv)
	})
}

func FuzzNormalizeDecimalString(f *testing.F) {
	f.Add("0")
	f.Add("-1.23")
	f.Add("9007199254740992")
	f.Fuzz(func(t *testing.T, s string) {
		nv, err := normalizeDecimalString(s)
		if err != nil {
			t.Fatalf("normalizeDecimalString: %v", err)
		}
		if nv.Kind != "decimal_string" {
			t.Fatalf("kind = %q, want decimal_string", nv.Kind)
		}
		if !nv.Exact {
			t.Fatal("decimal_string must be exact")
		}
		assertNormalizedValueJSONSafe(t, nv)
	})
}

func FuzzNormalizeValueFloat64(f *testing.F) {
	f.Add(uint64(0))
	f.Add(math.Float64bits(math.NaN()))
	f.Add(math.Float64bits(math.Inf(1)))
	f.Fuzz(func(t *testing.T, bits uint64) {
		fv := math.Float64frombits(bits)
		nv, err := normalizeValue(fv)
		if err != nil {
			t.Fatalf("normalizeValue(float64): %v", err)
		}
		assertNormalizedValueJSONSafe(t, nv)
	})
}

func FuzzApplyRedactorIdentity(f *testing.F) {
	f.Add([]byte("sample"))
	f.Fuzz(func(t *testing.T, data []byte) {
		in := any(string(data))
		out, err := applyRedactor(func(v any) (any, error) { return v, nil }, in)
		if err != nil {
			t.Fatalf("applyRedactor: %v", err)
		}
		if out != in {
			t.Fatalf("identity redactor changed value: %#v -> %#v", in, out)
		}
	})
}

func FuzzExportReportSampleNormalization(f *testing.F) {
	f.Add("value")
	f.Add("\x00\xff")
	f.Fuzz(func(t *testing.T, s string) {
		rep := Report{
			Results: []Result{{
				ID:             "fuzz",
				Kind:           KindCustom,
				Name:           "fuzz",
				Success:        false,
				RowDenominator: RowDenominatorAvailable,
				Total:          1,
				FailedCount:    1,
				SampleValues:   []any{s},
			}},
		}
		dto, err := ExportReport(rep, IncludeSamples())
		if err != nil {
			t.Fatalf("ExportReport: %v", err)
		}
		if _, err := json.Marshal(dto); err != nil {
			t.Fatalf("json.Marshal(ExportedReport): %v", err)
		}
	})
}
