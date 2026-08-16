package gxsql

import (
	"context"
	"testing"
)

func TestCompletenessRatePublishesCountsAndRate(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "a@example.com"},
		map[string]any{"id": int64(2), "email": nil},
		map[string]any{"id": int64(3), "email": "c@example.com"},
	))
	rep, err := NewSuite(Column("email").CompletenessRate().GreaterOrEqual(0.66)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.FailedPercent != 0 {
		t.Fatalf("completeness result = %#v", res)
	}
	facts := res.Facts.Completeness
	if facts == nil || facts.NonNullCount == nil || *facts.NonNullCount != 2 || facts.TotalCount == nil || *facts.TotalCount != 3 || facts.Rate == nil {
		t.Fatalf("completeness facts = %#v", facts)
	}
}
func TestRateBetweenPublishesBothBounds(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "a@example.com"},
		map[string]any{"id": int64(2), "email": nil},
		map[string]any{"id": int64(3), "email": "a@example.com"},
	))
	rep, err := NewSuite(
		Column("email").CompletenessRate().Between(0.5, 0.75),
		Column("email").DuplicateRate().Between(0.5, 0.75),
	).ValidateTable(context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	completeness := rep.Results[0].Facts.Completeness
	if completeness == nil || completeness.ConfiguredBound != nil ||
		completeness.ConfiguredLower == nil || *completeness.ConfiguredLower != 0.5 ||
		completeness.ConfiguredUpper == nil || *completeness.ConfiguredUpper != 0.75 {
		t.Fatalf("completeness bounds = %#v", completeness)
	}
	duplicates := rep.Results[1].Facts.DuplicateRate
	if duplicates == nil || duplicates.ConfiguredBound != nil ||
		duplicates.ConfiguredLower == nil || *duplicates.ConfiguredLower != 0.5 ||
		duplicates.ConfiguredUpper == nil || *duplicates.ConfiguredUpper != 0.75 {
		t.Fatalf("duplicate bounds = %#v", duplicates)
	}
}

func TestDuplicateRateUsesDuplicateRowsOverScopedRows(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "a@example.com"},
		map[string]any{"id": int64(2), "email": "a@example.com"},
		map[string]any{"id": int64(3), "email": "b@example.com"},
		map[string]any{"id": int64(4), "email": nil},
	))
	rep, err := NewSuite(Column("email").DuplicateRate().LessOrEqual(0.5)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.Facts.DuplicateRate == nil || res.Facts.DuplicateRate.DuplicateCount == nil || *res.Facts.DuplicateRate.DuplicateCount != 2 {
		t.Fatalf("duplicate rate result = %#v", res)
	}
}
