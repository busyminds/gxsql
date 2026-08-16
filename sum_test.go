package gxsql

import (
	"context"
	"testing"
)

func TestIntSumBetweenPublishesExactObservation(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "amount": int64(10)},
		map[string]any{"id": int64(2), "amount": int64(20)},
	))
	rep, err := NewSuite(Int("amount").SumBetween(0, 40)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success {
		t.Fatalf("expected sum bound to pass: %#v", res)
	}
	if res.Facts.Sum == nil || res.Facts.Sum.Observed == nil || *res.Facts.Sum.Observed != 30 {
		t.Fatalf("sum facts = %#v", res.Facts.Sum)
	}
}

func TestSumBetweenAllNullHasAbsentObservation(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "amount": nil},
		map[string]any{"id": int64(2), "amount": nil},
	))
	rep, err := NewSuite(Int("amount").SumBetween(0, 40)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.Facts.Sum == nil || res.Facts.Sum.Observed != nil {
		t.Fatalf("all-null sum = %#v", res)
	}
}
