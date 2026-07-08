package gxsql

import (
	"context"
	"strings"
	"testing"
)

func TestRowCountEmptyTableFailureShowsGotZero(t *testing.T) {
	setHarnessData(t, harnessUsers())
	db := openHarnessDB(t)

	rep, err := NewSuite(RowCount().GreaterThan(0)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success {
		t.Fatal("expected row count failure on empty table")
	}
	if res.Total != 0 {
		t.Fatalf("Total = %d, want 0 for table-level RowCount", res.Total)
	}
	if res.FailedCount != 0 {
		t.Fatalf("FailedCount = %d, want 0 for table-level RowCount", res.FailedCount)
	}
	if !strings.Contains(res.Name, "got 0") {
		t.Fatalf("Name = %q, want observed count appended", res.Name)
	}
	if res.Facts.ObservedCount == nil || *res.Facts.ObservedCount != 0 {
		t.Fatalf("Facts.ObservedCount = %v, want 0", res.Facts.ObservedCount)
	}
	if rep.Err() == nil {
		t.Fatal("expected report.Err() for validation failure")
	}
}

func TestRowCountEqualPasses(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1)},
		map[string]any{"id": int64(2)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(RowCount().Equal(2)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success {
		t.Fatalf("expected pass, got %#v", res)
	}
	if res.Total != 0 {
		t.Fatalf("table-level Total = %d, want 0", res.Total)
	}
	if !strings.Contains(res.Name, "got 2") {
		t.Fatalf("Name = %q, want observed count appended", res.Name)
	}
}

func TestRowCountBetweenFails(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1)},
		map[string]any{"id": int64(2)},
		map[string]any{"id": int64(3)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(RowCount().Between(1, 2)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Success {
		t.Fatal("expected row count failure")
	}
	if rep.Err() == nil {
		t.Fatal("expected report.Err() for validation failure")
	}
}

func TestRowCountGreaterOrEqual(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(RowCount().GreaterOrEqual(1)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatal("expected pass")
	}
}
