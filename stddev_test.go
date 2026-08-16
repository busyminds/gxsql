package gxsql

import (
	"context"
	"errors"
	"testing"
)

func TestStdDevBetweenUsesPopulationSemantics(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "amount": float64(1)},
		map[string]any{"id": int64(2), "amount": float64(2)},
		map[string]any{"id": int64(3), "amount": float64(3)},
	))
	rep, err := NewSuite(Float("amount").StdDevBetween(0.81, 0.82)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.Facts.PopulationStdDev == nil || res.Facts.PopulationStdDev.Observed == nil {
		t.Fatalf("stddev result = %#v", res)
	}
}

func TestStdDevBetweenRefusesUnsupportedSQLite(t *testing.T) {
	setHarnessData(t, harnessUsers(map[string]any{"id": int64(1), "amount": float64(1)}))
	_, err := NewSuite(Float("amount").StdDevBetween(0, 1)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"), WithDialect(SQLite()),
	)
	if err == nil || !errors.Is(err, ErrCategoryUnsupported) {
		t.Fatalf("expected unsupported stddev preflight error, got %v", err)
	}
}
