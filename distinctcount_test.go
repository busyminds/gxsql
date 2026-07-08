package gxsql

import (
	"context"
	"strings"
	"testing"
)

func TestDistinctCountEqualPasses(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active"},
		map[string]any{"id": int64(2), "status": "inactive"},
		map[string]any{"id": int64(3), "status": "active"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("status").DistinctCount().Equal(2)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success {
		t.Fatalf("expected pass, got %#v", res)
	}
	if res.Column != "status" {
		t.Fatalf("Column = %q, want status", res.Column)
	}
	if res.Total != 0 {
		t.Fatalf("table-level Total = %d, want 0", res.Total)
	}
	if !strings.Contains(res.Name, "got 2") {
		t.Fatalf("Name = %q, want observed count appended", res.Name)
	}
}

func TestDistinctCountBetweenFails(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "a"},
		map[string]any{"id": int64(2), "status": "b"},
		map[string]any{"id": int64(3), "status": "c"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("status").DistinctCount().Between(1, 2)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Success {
		t.Fatal("expected distinct count failure")
	}
	if !strings.Contains(rep.Results[0].Name, "got 3") {
		t.Fatalf("Name = %q, want observed count appended on failure", rep.Results[0].Name)
	}
	if rep.Err() == nil {
		t.Fatal("expected report.Err() for validation failure")
	}
}

func TestDistinctCountIgnoresNulls(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active"},
		map[string]any{"id": int64(2), "status": nil},
		map[string]any{"id": int64(3), "status": "active"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("status").DistinctCount().Equal(1)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("expected pass with one distinct non-null value, got %#v", rep.Results)
	}
}

func TestDistinctCountEmptyTable(t *testing.T) {
	setHarnessData(t, harnessUsers())
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("status").DistinctCount().Equal(0)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("empty table should have distinct count 0, got %#v", rep.Results)
	}
}

func TestDistinctCountAllNullColumnEvaluatesToZero(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": nil},
		map[string]any{"id": int64(2), "status": nil},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("status").DistinctCount().Equal(0)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success {
		t.Fatalf("all-null column should evaluate to distinct count 0, got %#v", res)
	}
	if !strings.Contains(res.Name, "got 0") {
		t.Fatalf("Name = %q, want observed count appended", res.Name)
	}

	rep, err = NewSuite(Column("status").DistinctCount().Equal(1)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res = rep.Results[0]
	if res.Success {
		t.Fatal("expected failure when distinct count is 0 but want 1")
	}
	if !strings.Contains(res.Name, "got 0") {
		t.Fatalf("Name = %q, want observed count appended on failure", res.Name)
	}
}

func TestDistinctCountGreaterOrEqual(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "a"},
		map[string]any{"id": int64(2), "status": "b"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("status").DistinctCount().GreaterOrEqual(2)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatal("expected pass")
	}
}

func TestDistinctCountLessOrEqual(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "a"},
		map[string]any{"id": int64(2), "status": "b"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("status").DistinctCount().LessOrEqual(2)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatal("expected pass")
	}
}
