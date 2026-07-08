package gxsql

import (
	"context"
	"reflect"
	"testing"
)

func TestUniqueEmptyTableVacuousPass(t *testing.T) {
	setHarnessData(t, harnessUsers())
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("email").Unique()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success {
		t.Fatal("empty table should pass Unique vacuously")
	}
	if res.Total != 0 || res.FailedCount != 0 {
		t.Fatalf("Total=%d FailedCount=%d, want 0 and 0", res.Total, res.FailedCount)
	}
}

func TestUniqueFlagsAllRowsInDuplicateGroups(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "a@b.com"},
		map[string]any{"id": int64(2), "email": "a@b.com"},
		map[string]any{"id": int64(3), "email": "unique@b.com"},
		map[string]any{"id": int64(4), "email": nil},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("email").Unique()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success {
		t.Fatal("expected unique failure")
	}
	if res.FailedCount != 2 {
		t.Fatalf("FailedCount = %d, want 2 (both duplicate rows, not gx first-pass semantics)", res.FailedCount)
	}
}

func TestUniquePassesWhenDistinct(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "a@b.com"},
		map[string]any{"id": int64(2), "email": "b@b.com"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("email").Unique()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatal("expected pass")
	}
}

func TestUniqueKeyModeReturnsDuplicateKeys(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "dup"},
		map[string]any{"id": int64(2), "email": "dup"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("email").Unique()).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []RowKey{{int64(1)}, {int64(2)}}
	if !reflect.DeepEqual(rep.Results[0].FailedKeys, want) {
		t.Fatalf("FailedKeys = %#v, want %#v", rep.Results[0].FailedKeys, want)
	}
}
