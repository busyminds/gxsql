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

func TestGenericKindCustomStringUnchanged(t *testing.T) {
	res := Result{
		Kind:           KindCustom,
		Name:           "value probe",
		Success:        false,
		RowDenominator: RowDenominatorAvailable,
		Total:          10,
		FailedCount:    3,
		FailedPercent:  30,
		SampleValues:   []any{"a"},
		FailedKeys:     []RowKey{{int64(1)}},
		// Uncomparable dynamic field must not affect generic KindCustom rendering.
		Facts: ResultFacts{ConfiguredBound: []string{"probe"}},
	}
	got := res.String()
	if strings.Contains(got, "3 failed") && !strings.Contains(got, "3/10 failed") {
		t.Fatalf("generic KindCustom used compact custom-count render: %q", got)
	}
	if !strings.Contains(got, "3/10 failed (30.0%)") {
		t.Fatalf("generic KindCustom missing row denominator render: %q", got)
	}
}

func TestResultStringReconcileCountsUnequal(t *testing.T) {
	res := Result{
		Kind:           KindReconcileCountsEqual,
		Name:           "reconcile counts equal orders: left=2 right=1",
		Success:        false,
		RowDenominator: RowDenominatorUnavailable,
		FailedCount:    1,
		Facts: ResultFacts{Reconcile: &ReconcileFacts{
			Left:               Table("users"),
			Right:              Table("orders"),
			ObservedLeftCount:  intFact(2),
			ObservedRightCount: intFact(1),
			Relationship:       reconcileRelationshipEqual,
		}},
		shape: resultShapeReconcileCounts,
	}

	got := res.String()
	want := "✗ reconcile counts equal orders: left=2 right=1  1 failed"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if !strings.Contains(got, "1 failed") {
		t.Fatalf("String() = %q missing failure count", got)
	}
	for _, banned := range []string{"1/0", "%", "e.g.", " @ "} {
		if strings.Contains(got, banned) {
			t.Fatalf("String() = %q contains banned %q", got, banned)
		}
	}
}

func TestResultStringReconcileCountsEqual(t *testing.T) {
	res := Result{
		Kind:           KindReconcileCountsEqual,
		Name:           "reconcile counts equal orders: left=2 right=2",
		Success:        true,
		RowDenominator: RowDenominatorUnavailable,
		FailedCount:    0,
		Facts: ResultFacts{Reconcile: &ReconcileFacts{
			Left:               Table("users"),
			Right:              Table("orders"),
			ObservedLeftCount:  intFact(2),
			ObservedRightCount: intFact(2),
			Relationship:       reconcileRelationshipEqual,
		}},
		shape: resultShapeReconcileCounts,
	}
	got := res.String()
	want := "✓ reconcile counts equal orders: left=2 right=2"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
