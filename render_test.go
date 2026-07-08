package gxsql

import (
	"strings"
	"testing"
)

func TestResultStringPassAndFail(t *testing.T) {
	pass := Result{Name: "id unique", Success: true, Total: 100}
	if got := pass.String(); !strings.HasPrefix(got, "✓ id unique") {
		t.Fatalf("pass render = %q", got)
	}

	fail := Result{
		Name: "email not empty", Success: false, Total: 100,
		FailedCount: 12, FailedPercent: 12.0,
		SampleValues: []any{"", "x"},
		FailedKeys:   []RowKey{{int64(1)}, {int64(9)}},
	}
	got := fail.String()
	if !strings.HasPrefix(got, "✗ email not empty") {
		t.Fatalf("fail render missing mark/name: %q", got)
	}
	if !strings.Contains(got, "12/100 failed (12.0%)") {
		t.Fatalf("fail render missing counts: %q", got)
	}
}

func TestResultStringTruncatesSamplesAndKeys(t *testing.T) {
	samples := make([]any, 15)
	keys := make([]RowKey, 15)
	for i := range samples {
		samples[i] = i
		keys[i] = RowKey{int64(i)}
	}
	r := Result{
		Name: "x", Success: false, Total: 15, FailedCount: 15,
		SampleValues: samples, FailedKeys: keys,
	}
	if !strings.Contains(r.String(), "…") {
		t.Fatalf("expected truncation ellipsis")
	}
}

func TestReportStringHeader(t *testing.T) {
	rep := Report{Results: []Result{
		{Name: "a", Success: true, Total: 1},
		{Name: "b", Success: false, Total: 1, FailedCount: 1, FailedPercent: 100},
	}}
	got := rep.String()
	if !strings.HasPrefix(got, "gxsql report: 1/2 expectations passed") {
		t.Fatalf("report header = %q", got)
	}
}
