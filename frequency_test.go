package gxsql

import (
	"context"
	"testing"
)

func TestFrequencyIncludesNullAsCategory(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "ready"},
		map[string]any{"id": int64(2), "status": nil},
		map[string]any{"id": int64(3), "status": nil},
	))
	rep, err := NewSuite(Column("status").Frequency(nil).GreaterOrEqual(0.66)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.Facts.Frequency == nil || res.Facts.Frequency.ValueCount == nil || *res.Facts.Frequency.ValueCount != 2 {
		t.Fatalf("frequency result = %#v", res)
	}
}
func TestFrequencyBetweenPublishesBothBounds(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "ready"},
		map[string]any{"id": int64(2), "status": "queued"},
	))
	rep, err := NewSuite(
		Column("status").Frequency("ready").Between(0.4, 0.6),
		Column("status").DominantShare().Between(0.4, 0.6),
	).ValidateTable(context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	frequency := rep.Results[0].Facts.Frequency
	if frequency == nil || frequency.ConfiguredBound != nil || frequency.Share == nil ||
		frequency.ConfiguredLower == nil || *frequency.ConfiguredLower != 0.4 ||
		frequency.ConfiguredUpper == nil || *frequency.ConfiguredUpper != 0.6 {
		t.Fatalf("frequency bounds = %#v", frequency)
	}
	dominant := rep.Results[1].Facts.DominantShare
	if dominant == nil || dominant.ConfiguredBound != nil || dominant.Share == nil ||
		dominant.ConfiguredLower == nil || *dominant.ConfiguredLower != 0.4 ||
		dominant.ConfiguredUpper == nil || *dominant.ConfiguredUpper != 0.6 {
		t.Fatalf("dominant bounds = %#v", dominant)
	}
}

func TestDominantSharePublishesTieCount(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "ready"},
		map[string]any{"id": int64(2), "status": "ready"},
		map[string]any{"id": int64(3), "status": "queued"},
		map[string]any{"id": int64(4), "status": "queued"},
	))
	rep, err := NewSuite(Column("status").DominantShare().LessOrEqual(0.5)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.Facts.DominantShare == nil || res.Facts.DominantShare.TieCount == nil || *res.Facts.DominantShare.TieCount != 2 {
		t.Fatalf("dominant result = %#v", res)
	}
}
