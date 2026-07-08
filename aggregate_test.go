package gxsql

import (
	"context"
	"testing"
)

func TestAggregateAverageBetween(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "amount": float64(10)},
		map[string]any{"id": int64(2), "amount": float64(30)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Float("amount").AverageBetween(1, 100)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Results[0].Success {
		t.Fatalf("expected pass, got %#v", rep.Results[0])
	}
}

func TestAggregateMinMaxBounds(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "amount": float64(5)},
		map[string]any{"id": int64(2), "amount": float64(50)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		Float("amount").MinGreaterOrEqual(0),
		Float("amount").MaxLessOrEqual(100),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("expected pass, got %#v", rep.Results)
	}
}

func TestAggregateAllNullColumnVacuousPass(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "amount": nil},
		map[string]any{"id": int64(2), "amount": nil},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		Float("amount").AverageBetween(1, 100),
		Float("amount").MinGreaterOrEqual(0),
		Float("amount").MaxLessOrEqual(100),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("all-null column should pass aggregates vacuously, got %#v", rep.Results)
	}
	wantNames := []string{
		"amount average in [1,100]",
		"amount min >= 0",
		"amount max <= 100",
	}
	for i, res := range rep.Results {
		if !res.Success {
			t.Fatalf("result[%d] should pass: %#v", i, res)
		}
		if res.Name != wantNames[i] {
			t.Fatalf("result[%d] Name=%q, want %q", i, res.Name, wantNames[i])
		}
		if res.Total != 0 || res.FailedCount != 0 {
			t.Fatalf("result[%d] Total=%d FailedCount=%d, want table-level zeros", i, res.Total, res.FailedCount)
		}
	}
}
