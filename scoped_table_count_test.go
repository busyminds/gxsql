package gxsql

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func evalTableLevelWithScope(t *testing.T, db DB, exp Expectation, scope trustedScope, opts ...func(*evalOptions)) Result {
	t.Helper()
	evalOpts := evalOptions{
		dialect: Postgres(),
		scope:   &scope,
	}
	for _, fn := range opts {
		fn(&evalOpts)
	}
	res, err := exp.evaluateSQL(context.Background(), db, Table("users"), evalOpts)
	if err != nil {
		t.Fatalf("evaluateSQL: %v", err)
	}
	return res
}

func assertTableLevelDenominatorUnavailable(t *testing.T, res Result) {
	t.Helper()
	if res.RowDenominator != RowDenominatorUnavailable {
		t.Fatalf("RowDenominator = %q, want %q", res.RowDenominator, RowDenominatorUnavailable)
	}
	if res.Total != 0 {
		t.Fatalf("Total = %d, want 0 for table-level result", res.Total)
	}
	if res.FailedCount != 0 {
		t.Fatalf("FailedCount = %d, want 0 for table-level result", res.FailedCount)
	}
	if res.FailedPercent != 0 {
		t.Fatalf("FailedPercent = %v, want 0 for table-level result", res.FailedPercent)
	}
}

func assertObservedCount(t *testing.T, res Result, want int) {
	t.Helper()
	if res.Facts.ObservedCount == nil || *res.Facts.ObservedCount != want {
		t.Fatalf("Facts.ObservedCount = %v, want %d", res.Facts.ObservedCount, want)
	}
	if !strings.Contains(res.Name, "got "+fmt.Sprint(want)) {
		t.Fatalf("Name = %q, want observed count %d appended", res.Name, want)
	}
}

func assertScopedTableCountQuery(t *testing.T, q recordedQuery, scope trustedScope) {
	t.Helper()
	assertCountQuery(t, q)
	assertScopeQuery(t, q, scope, false)
}

func assertScopedDistinctCountQuery(t *testing.T, q recordedQuery, scope trustedScope, column string) {
	t.Helper()
	upper := strings.ToUpper(q.text)
	if !strings.Contains(upper, "COUNT(DISTINCT") {
		t.Fatalf("expected COUNT(DISTINCT) query, got %q", q.text)
	}
	if !strings.Contains(q.text, `"`+column+`"`) {
		t.Fatalf("expected distinct column %q in %q", column, q.text)
	}
	assertScopeQuery(t, q, scope, false)
}

func TestRowCountScopeComposesScopedCountQuery(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1)},
		map[string]any{"id": int64(2), "tenant_id": "t2"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	_ = evalTableLevelWithScope(t, db, RowCount().Equal(1), scope)

	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1 scoped COUNT(*)", len(db.queries))
	}
	assertScopedTableCountQuery(t, db.queries[0], scope)
}

func TestDistinctCountScopeComposesScopedCountQuery(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "status": "active"},
		map[string]any{"id": int64(2), "tenant_id": "t2", "status": "inactive"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	_ = evalTableLevelWithScope(t, db, Column("status").DistinctCount().Equal(1), scope)

	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1 scoped COUNT(DISTINCT)", len(db.queries))
	}
	assertScopedDistinctCountQuery(t, db.queries[0], scope, "status")
}

func TestRowCountScopedObservedCount(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1)},
		map[string]any{"id": int64(2)},
		map[string]any{"id": int64(3), "tenant_id": "t2"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openHarnessDB(t)

	res := evalTableLevelWithScope(t, db, RowCount().Equal(2), scope)

	if !res.Success {
		t.Fatalf("expected scoped row count pass, got %#v", res)
	}
	assertTableLevelDenominatorUnavailable(t, res)
	assertObservedCount(t, res, 2)
}

func TestRowCountScopedExcludesOutOfScopeRows(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1)},
		map[string]any{"id": int64(2)},
		map[string]any{"id": int64(3), "tenant_id": "t2"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openHarnessDB(t)

	res := evalTableLevelWithScope(t, db, RowCount().Equal(3), scope)

	if res.Success {
		t.Fatal("expected scoped row count failure when whole-table count differs")
	}
	assertTableLevelDenominatorUnavailable(t, res)
	assertObservedCount(t, res, 2)
}

func TestDistinctCountScopedExcludesNullAndOutOfScope(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "status": "active"},
		map[string]any{"id": int64(2), "status": "active"},
		map[string]any{"id": int64(3), "status": nil},
		map[string]any{"id": int64(4), "tenant_id": "t2", "status": "inactive"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openHarnessDB(t)

	res := evalTableLevelWithScope(t, db, Column("status").DistinctCount().Equal(1), scope)

	if !res.Success {
		t.Fatalf("expected scoped distinct count pass, got %#v", res)
	}
	if res.Column != "status" {
		t.Fatalf("Column = %q, want status", res.Column)
	}
	assertTableLevelDenominatorUnavailable(t, res)
	assertObservedCount(t, res, 1)
}

func TestRowCountEmptyScopeEqualZeroPasses(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1)},
		map[string]any{"id": int64(2), "tenant_id": "t2"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "nobody")
	db := openHarnessDB(t)

	res := evalTableLevelWithScope(t, db, RowCount().Equal(0), scope)

	if !res.Success {
		t.Fatalf("expected empty scoped row count to pass Equal(0), got %#v", res)
	}
	assertTableLevelDenominatorUnavailable(t, res)
	assertObservedCount(t, res, 0)
}

func TestRowCountEmptyScopePositiveBoundsFail(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1)},
	))
	scope := mustTestScope(t, "tenant_id = ?", "nobody")
	db := openHarnessDB(t)

	cases := []struct {
		name string
		exp  Expectation
	}{
		{name: "greater than zero", exp: RowCount().GreaterThan(0)},
		{name: "between excluding zero", exp: RowCount().Between(1, 5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := evalTableLevelWithScope(t, db, tc.exp, scope)
			if res.Success {
				t.Fatalf("expected empty scoped row count failure for %s", tc.name)
			}
			assertTableLevelDenominatorUnavailable(t, res)
			assertObservedCount(t, res, 0)
		})
	}
}

func TestDistinctCountEmptyScopeEqualZeroPasses(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "status": "active"},
		map[string]any{"id": int64(2), "status": "inactive"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "nobody")
	db := openHarnessDB(t)

	res := evalTableLevelWithScope(t, db, Column("status").DistinctCount().Equal(0), scope)

	if !res.Success {
		t.Fatalf("expected empty scoped distinct count to pass Equal(0), got %#v", res)
	}
	assertTableLevelDenominatorUnavailable(t, res)
	assertObservedCount(t, res, 0)
}

func TestDistinctCountEmptyScopePositiveBoundsFail(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "status": "active"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "nobody")
	db := openHarnessDB(t)

	cases := []struct {
		name string
		exp  Expectation
	}{
		{name: "greater or equal one", exp: Column("status").DistinctCount().GreaterOrEqual(1)},
		{name: "between excluding zero", exp: Column("status").DistinctCount().Between(1, 3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := evalTableLevelWithScope(t, db, tc.exp, scope)
			if res.Success {
				t.Fatalf("expected empty scoped distinct count failure for %s", tc.name)
			}
			assertTableLevelDenominatorUnavailable(t, res)
			assertObservedCount(t, res, 0)
		})
	}
}

func TestTableLevelNoScopePreservesUnscopedRowCount(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1)},
		map[string]any{"id": int64(2)},
		map[string]any{"id": int64(3)},
	))
	db := openHarnessDB(t)

	res, err := RowCount().Equal(3).evaluateSQL(context.Background(), db, Table("users"), evalOptions{
		dialect: Postgres(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("expected unscoped row count pass, got %#v", res)
	}
	assertTableLevelDenominatorUnavailable(t, res)
	assertObservedCount(t, res, 3)
}
