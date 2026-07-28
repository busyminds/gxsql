package gxsql

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestToleranceSemanticsBoundaryBelowAboveZero(t *testing.T) {
	t.Parallel()

	const max = 2
	samples := []any{int64(150), int64(200)}
	keys := []RowKey{{int64(2)}, {int64(4)}}

	cases := []struct {
		name          string
		failed        int
		wantSuccess   bool
		wantTolerated bool
	}{
		{name: "boundary", failed: 2, wantSuccess: true, wantTolerated: true},
		{name: "below", failed: 1, wantSuccess: true, wantTolerated: true},
		{name: "above", failed: 3, wantSuccess: false, wantTolerated: false},
		{name: "zero", failed: 0, wantSuccess: true, wantTolerated: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := perRowResult(KindBetween, "age", "age between [0,120]", 10, tc.failed, ResultFacts{
				ConfiguredBoundLower: 0,
				ConfiguredBoundUpper: 120,
			})
			if tc.failed > 0 {
				n := tc.failed
				if n > len(samples) {
					n = len(samples)
				}
				raw.SampleValues = append([]any(nil), samples[:n]...)
				raw.FailedKeys = append([]RowKey(nil), keys[:n]...)
			}
			raw.ID = "age-check"

			got := applyMaxFailedCount(raw, max)
			if got.Success != tc.wantSuccess {
				t.Fatalf("Success = %v, want %v", got.Success, tc.wantSuccess)
			}
			if got.Tolerated != tc.wantTolerated {
				t.Fatalf("tolerated = %v, want %v", got.Tolerated, tc.wantTolerated)
			}
			if got.Facts.ConfiguredMaxFailedCount == nil || *got.Facts.ConfiguredMaxFailedCount != max {
				t.Fatalf("ConfiguredMaxFailedCount = %v, want %d", got.Facts.ConfiguredMaxFailedCount, max)
			}
			assertRawObservationsPreserved(t, raw, got)
		})
	}
}

func TestToleranceEmptyPopulation(t *testing.T) {
	t.Parallel()

	raw := perRowResult(KindBetween, "age", "age between [0,120]", 0, 0, ResultFacts{})
	got := applyMaxFailedCount(raw, 5)
	if !got.Success {
		t.Fatal("empty population must pass")
	}
	if got.Tolerated {
		t.Fatal("empty population must not be tolerated")
	}
	if got.Total != 0 || got.FailedCount != 0 || got.FailedPercent != 0 {
		t.Fatalf("empty observations = Total=%d FailedCount=%d FailedPercent=%v, want zeros",
			got.Total, got.FailedCount, got.FailedPercent)
	}
	if got.Facts.ConfiguredMaxFailedCount == nil || *got.Facts.ConfiguredMaxFailedCount != 5 {
		t.Fatalf("ConfiguredMaxFailedCount = %v, want 5", got.Facts.ConfiguredMaxFailedCount)
	}
}

func TestToleranceSemanticsRawPreservation(t *testing.T) {
	t.Parallel()

	raw := perRowResult(KindNotNull, "email", "email is not null", 100, 4, ResultFacts{})
	raw.ID = "email-nn"
	raw.SampleValues = []any{nil, nil}
	raw.FailedKeys = []RowKey{{int64(7)}, {int64(9)}}

	got := applyMaxFailedCount(raw, 4)
	if !got.Success || !got.Tolerated {
		t.Fatalf("Success=%v tolerated=%v, want true/true on exact boundary", got.Success, got.Tolerated)
	}
	assertRawObservationsPreserved(t, raw, got)
}

func TestToleranceErrorNeverTolerated(t *testing.T) {
	t.Parallel()

	raw := perRowResult(KindBetween, "age", "age between [0,120]", 10, 1, ResultFacts{})
	raw.SampleValues = []any{int64(999)}
	raw.FailedKeys = []RowKey{{int64(1)}}
	raw.Err = errors.New("scan failed")
	raw.Success = false

	got := applyMaxFailedCount(raw, 5)
	if got.Success {
		t.Fatal("error results must remain unsuccessful")
	}
	if got.Tolerated {
		t.Fatal("error results must never be tolerated")
	}
	if got.Err != raw.Err {
		t.Fatalf("Err = %v, want preserved %v", got.Err, raw.Err)
	}
	if got.Facts.ConfiguredMaxFailedCount == nil || *got.Facts.ConfiguredMaxFailedCount != 5 {
		t.Fatalf("ConfiguredMaxFailedCount = %v, want 5", got.Facts.ConfiguredMaxFailedCount)
	}
	assertRawObservationsPreserved(t, raw, got)
}

func TestMaxFailedCountEvaluateSQLSeparateErrorNeverTolerated(t *testing.T) {
	t.Parallel()

	innerErr := errors.New("sample load failed")
	raw := perRowResult(KindBetween, "age", "age between [0,120]", 10, 1, ResultFacts{})
	raw.SampleValues = []any{int64(999)}
	if raw.Err != nil || raw.RowDenominator != RowDenominatorAvailable || raw.FailedCount != 1 {
		t.Fatalf("fixture = %#v, want measured within-bound result with nil Err", raw)
	}

	exp := &maxFailedCountExpectation{
		max:   5,
		inner: measuredErrorExpectation{name: raw.Name, res: raw, err: innerErr},
	}
	got, err := exp.evaluateSQL(context.Background(), nil, Table("users"), evalOptions{})
	if err != innerErr {
		t.Fatalf("err = %v, want original separate error", err)
	}
	if got.Success || got.Tolerated {
		t.Fatalf("Success=%v Tolerated=%v, separate err must not become a policy pass", got.Success, got.Tolerated)
	}
	if got.Err != innerErr {
		t.Fatalf("Err = %v, want stamped separate error before decoration", got.Err)
	}
	if got.Facts.ConfiguredMaxFailedCount == nil || *got.Facts.ConfiguredMaxFailedCount != 5 {
		t.Fatalf("ConfiguredMaxFailedCount = %v, want 5", got.Facts.ConfiguredMaxFailedCount)
	}
	if got.FailedCount != 1 || got.Total != 10 {
		t.Fatalf("raw observations changed: FailedCount=%d Total=%d", got.FailedCount, got.Total)
	}
}

type measuredErrorExpectation struct {
	name string
	res  Result
	err  error
}

func (e measuredErrorExpectation) Name() string { return e.name }

func (e measuredErrorExpectation) evaluateSQL(
	context.Context, DB, TableRef, evalOptions,
) (Result, error) {
	return e.res, e.err
}

func TestToleranceSemanticsUnavailableDenominator(t *testing.T) {
	t.Parallel()

	raw := tableLevelResult(KindRowCountEqual, "", "row count == 10", false, ResultFacts{
		ObservedCount:   intFact(12),
		ConfiguredCount: intFact(10),
	})
	raw.FailedCount = 1 // custom-style count noise must not become tolerated

	got := applyMaxFailedCount(raw, 10)
	if got.Success != raw.Success {
		t.Fatalf("Success = %v, want unchanged %v", got.Success, raw.Success)
	}
	if got.Tolerated {
		t.Fatal("unavailable-denominator results must never be tolerated")
	}
	if got.Facts.ConfiguredMaxFailedCount == nil || *got.Facts.ConfiguredMaxFailedCount != 10 {
		t.Fatalf("ConfiguredMaxFailedCount = %v, want 10", got.Facts.ConfiguredMaxFailedCount)
	}
}

func TestToleranceSemanticsUnwrappedHasNoState(t *testing.T) {
	t.Parallel()

	raw := perRowResult(KindBetween, "age", "age between [0,120]", 10, 2, ResultFacts{})
	if raw.Tolerated {
		t.Fatal("unwrapped result must not be tolerated")
	}
	if raw.Facts.ConfiguredMaxFailedCount != nil {
		t.Fatalf("unwrapped ConfiguredMaxFailedCount = %v, want nil", raw.Facts.ConfiguredMaxFailedCount)
	}
}

func assertRawObservationsPreserved(t *testing.T, raw, got Result) {
	t.Helper()
	if got.ID != raw.ID {
		t.Fatalf("ID = %q, want %q", got.ID, raw.ID)
	}
	if got.Kind != raw.Kind {
		t.Fatalf("Kind = %q, want %q", got.Kind, raw.Kind)
	}
	if got.Name != raw.Name {
		t.Fatalf("Name = %q, want %q", got.Name, raw.Name)
	}
	if got.Column != raw.Column {
		t.Fatalf("Column = %q, want %q", got.Column, raw.Column)
	}
	if got.RowDenominator != raw.RowDenominator {
		t.Fatalf("RowDenominator = %q, want %q", got.RowDenominator, raw.RowDenominator)
	}
	if got.Total != raw.Total {
		t.Fatalf("Total = %d, want %d", got.Total, raw.Total)
	}
	if got.FailedCount != raw.FailedCount {
		t.Fatalf("FailedCount = %d, want %d", got.FailedCount, raw.FailedCount)
	}
	if got.FailedPercent != raw.FailedPercent {
		t.Fatalf("FailedPercent = %v, want %v", got.FailedPercent, raw.FailedPercent)
	}
	if !reflect.DeepEqual(got.SampleValues, raw.SampleValues) {
		t.Fatalf("SampleValues = %#v, want %#v", got.SampleValues, raw.SampleValues)
	}
	if !reflect.DeepEqual(got.FailedKeys, raw.FailedKeys) {
		t.Fatalf("FailedKeys = %#v, want %#v", got.FailedKeys, raw.FailedKeys)
	}
	if got.Facts.ConfiguredBoundLower != raw.Facts.ConfiguredBoundLower ||
		got.Facts.ConfiguredBoundUpper != raw.Facts.ConfiguredBoundUpper ||
		!reflect.DeepEqual(got.Facts.ObservedCount, raw.Facts.ObservedCount) ||
		!reflect.DeepEqual(got.Facts.ConfiguredCount, raw.Facts.ConfiguredCount) {
		t.Fatalf("non-tolerance Facts changed: got %#v want %#v", got.Facts, raw.Facts)
	}
}
