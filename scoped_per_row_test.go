package gxsql

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

type recordedQuery struct {
	text string
	args []any
}

type recordingDB struct {
	DB
	queries []recordedQuery
}

func (r *recordingDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.record(query, args...)
	return r.DB.QueryContext(ctx, query, args...)
}

func (r *recordingDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	r.record(query, args...)
	return r.DB.QueryRowContext(ctx, query, args...)
}

func (r *recordingDB) record(query string, args ...any) {
	r.queries = append(r.queries, recordedQuery{
		text: collapseSpaces(query),
		args: append([]any(nil), args...),
	})
}

func openRecordingHarnessDB(t *testing.T) *recordingDB {
	t.Helper()
	return &recordingDB{DB: openHarnessDB(t)}
}

func mustTestScope(t *testing.T, predicate string, values ...any) trustedScope {
	t.Helper()
	scope, err := newTrustedScope("scoped-test", predicate, values)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func scopedHarnessUsers(scopeCol, defaultScopeVal string, rows ...map[string]any) map[string][]map[string]any {
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		copied := make(map[string]any, len(row)+1)
		for k, v := range row {
			copied[k] = v
		}
		if _, ok := copied[scopeCol]; !ok {
			copied[scopeCol] = defaultScopeVal
		}
		out[i] = copied
	}
	return map[string][]map[string]any{"users": out}
}

func evalPerRowWithScope(t *testing.T, db DB, exp Expectation, scope trustedScope, opts ...func(*evalOptions)) Result {
	t.Helper()
	evalOpts := evalOptions{
		dialect:       Postgres(),
		sampleCap:     DefaultSampleCap,
		failedKeysCap: DefaultFailedKeysCap,
		scope:         &scope,
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

func renderedScopeFragment(t *testing.T, scope trustedScope) string {
	t.Helper()
	pred, err := scope.render(Postgres())
	if err != nil {
		t.Fatal(err)
	}
	return "(" + pred.where + ")"
}

func assertScopeQuery(t *testing.T, q recordedQuery, scope trustedScope, withFail bool) {
	t.Helper()
	frag := renderedScopeFragment(t, scope)
	if !strings.Contains(q.text, frag) {
		t.Fatalf("query missing scope fragment %q:\n%s", frag, q.text)
	}
	if withFail && !strings.Contains(q.text, ") AND (") {
		t.Fatalf("query missing scoped failure composition:\n%s", q.text)
	}
	if !withFail && strings.Contains(q.text, ") AND (") {
		t.Fatalf("total count query must not include failure predicate:\n%s", q.text)
	}
	pred, err := scope.render(Postgres())
	if err != nil {
		t.Fatal(err)
	}
	if len(q.args) < len(pred.args) {
		t.Fatalf("args %v missing scope prefix (want %d values)", q.args, len(pred.args))
	}
	for i, want := range pred.args {
		if !valuesEqual(q.args[i], want) {
			t.Fatalf("scope arg[%d] = %v, want %v (all args %v)", i, q.args[i], want, q.args)
		}
	}
}

func assertCountQuery(t *testing.T, q recordedQuery) {
	t.Helper()
	if !strings.Contains(strings.ToUpper(q.text), "SELECT COUNT(*)") {
		t.Fatalf("expected COUNT query, got %q", q.text)
	}
}

func assertSampleQuery(t *testing.T, q recordedQuery, column string) {
	t.Helper()
	if !strings.Contains(q.text, " LIMIT ") {
		t.Fatalf("expected sample query with LIMIT, got %q", q.text)
	}
	if !strings.Contains(q.text, `"`+column+`"`) {
		t.Fatalf("expected sample column %q in %q", column, q.text)
	}
}

func assertFailedKeyQuery(t *testing.T, q recordedQuery, keyCol string) {
	t.Helper()
	if strings.Contains(strings.ToUpper(q.text), "COUNT(") {
		t.Fatalf("expected failed-key SELECT, got %q", q.text)
	}
	if !strings.Contains(q.text, `"`+keyCol+`"`) {
		t.Fatalf("expected key column %q in %q", keyCol, q.text)
	}
}

func assertPerRowScopedQueryPlan(t *testing.T, db *recordingDB, scope trustedScope, column, keyCol string) {
	t.Helper()
	if len(db.queries) < 4 {
		t.Fatalf("queries = %d, want at least 4 (total, failed, sample, keys)", len(db.queries))
	}
	assertCountQuery(t, db.queries[0])
	assertScopeQuery(t, db.queries[0], scope, false)
	assertCountQuery(t, db.queries[1])
	assertScopeQuery(t, db.queries[1], scope, true)
	assertSampleQuery(t, db.queries[2], column)
	assertScopeQuery(t, db.queries[2], scope, true)
	assertFailedKeyQuery(t, db.queries[3], keyCol)
	assertScopeQuery(t, db.queries[3], scope, true)
}

func TestPerRowScopeBetweenComposesScopedQueries(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(150)},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(10)},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	_ = evalPerRowWithScope(t, db, Int("age").Between(0, 120), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
		o.sampleCap = 5
	})

	assertPerRowScopedQueryPlan(t, db, scope, "age", "id")
}

func TestPerRowScopeInComposesScopedQueries(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "status": "active"},
		map[string]any{"id": int64(2), "status": "deleted"},
		map[string]any{"id": int64(3), "status": nil},
		map[string]any{"id": int64(4), "tenant_id": "t2", "status": "deleted"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	_ = evalPerRowWithScope(t, db, Column("status").In("active", "pending"), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
		o.sampleCap = 5
	})

	assertPerRowScopedQueryPlan(t, db, scope, "status", "id")
}

func TestPerRowScopeNotNullComposesScopedQueries(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "deleted_at": nil},
		map[string]any{"id": int64(2), "deleted_at": "2024-01-01"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "deleted_at": nil},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	_ = evalPerRowWithScope(t, db, Column("deleted_at").NotNull(), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
		o.sampleCap = 5
	})

	assertPerRowScopedQueryPlan(t, db, scope, "deleted_at", "id")
}

func TestPerRowScopeLenEqualComposesScopedQueries(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "country_code": "US"},
		map[string]any{"id": int64(2), "country_code": "USA"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "country_code": "X"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	_ = evalPerRowWithScope(t, db, String("country_code").LenEqual(2), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
		o.sampleCap = 5
	})

	assertPerRowScopedQueryPlan(t, db, scope, "country_code", "id")
}

func TestPerRowScopeBetweenScopedTotals(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(150)},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(10)},
		map[string]any{"id": int64(4), "age": nil},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Int("age").Between(0, 120), scope)

	if res.Total != 3 {
		t.Fatalf("Total = %d, want 3 scoped rows", res.Total)
	}
	if res.FailedCount != 2 {
		t.Fatalf("FailedCount = %d, want 2 scoped failures", res.FailedCount)
	}
	wantPercent := float64(res.FailedCount) / float64(res.Total) * 100
	if res.FailedPercent != wantPercent {
		t.Fatalf("FailedPercent = %v, want %v from scoped denominator", res.FailedPercent, wantPercent)
	}
}

func TestPerRowScopeZeroScopedRowsEmptyResults(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(30)},
	))
	scope := mustTestScope(t, "tenant_id = ?", "nobody")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Int("age").Between(0, 120), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
		o.sampleCap = 5
	})

	if res.Total != 0 {
		t.Fatalf("Total = %d, want 0", res.Total)
	}
	if res.FailedCount != 0 {
		t.Fatalf("FailedCount = %d, want 0", res.FailedCount)
	}
	if res.FailedPercent != 0 {
		t.Fatalf("FailedPercent = %v, want 0", res.FailedPercent)
	}
	if len(res.SampleValues) != 0 {
		t.Fatalf("SampleValues = %#v, want empty", res.SampleValues)
	}
	if len(res.FailedKeys) != 0 {
		t.Fatalf("FailedKeys = %#v, want empty", res.FailedKeys)
	}
	if len(db.queries) != 2 {
		t.Fatalf("queries = %d, want 2 (total and failed counts only)", len(db.queries))
	}
	assertScopeQuery(t, db.queries[0], scope, false)
	assertScopeQuery(t, db.queries[1], scope, true)
}

func TestPerRowScopeZeroFailuresSkipSampleAndKeyQueries(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "tenant_id": "t2", "age": int64(200)},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Int("age").Between(0, 120), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
		o.sampleCap = 5
	})

	if res.Total != 1 {
		t.Fatalf("Total = %d, want 1 scoped row", res.Total)
	}
	if res.FailedCount != 0 {
		t.Fatalf("FailedCount = %d, want 0 under scope", res.FailedCount)
	}
	if len(res.SampleValues) != 0 {
		t.Fatalf("SampleValues = %#v, want empty when FailedCount is 0", res.SampleValues)
	}
	if len(res.FailedKeys) != 0 {
		t.Fatalf("FailedKeys = %#v, want empty when FailedCount is 0", res.FailedKeys)
	}
	if len(db.queries) != 2 {
		t.Fatalf("queries = %d, want 2 (skip sample and key queries)", len(db.queries))
	}
}

func TestPerRowSampleCapUnderScope(t *testing.T) {
	rows := make([]map[string]any, 6)
	for i := range rows {
		rows[i] = map[string]any{"id": int64(i + 1), "tenant_id": "t1", "age": int64(200)}
	}
	setHarnessData(t, map[string][]map[string]any{"users": rows})
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Int("age").Between(0, 120), scope, func(o *evalOptions) {
		o.sampleCap = 3
	})

	if res.Total != 6 {
		t.Fatalf("Total = %d, want 6 scoped rows", res.Total)
	}
	if res.FailedCount != 6 {
		t.Fatalf("FailedCount = %d, want complete scoped failure count", res.FailedCount)
	}
	if len(res.SampleValues) > 3 {
		t.Fatalf("SampleValues len = %d, want <= 3", len(res.SampleValues))
	}
	if len(db.queries) < 3 {
		t.Fatalf("queries = %d, want sample query after scoped counts", len(db.queries))
	}
	assertSampleQuery(t, db.queries[2], "age")
	assertScopeQuery(t, db.queries[2], scope, true)
}

func TestFailedKeyScopeQueriesIncludeScope(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(200)},
		map[string]any{"id": int64(3), "age": int64(300)},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Int("age").Between(0, 120), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
		o.sampleCap = 0
	})

	if len(db.queries) != 3 {
		t.Fatalf("queries = %d, want 3 (total, failed, keys)", len(db.queries))
	}
	assertFailedKeyQuery(t, db.queries[2], "id")
	assertScopeQuery(t, db.queries[2], scope, true)
	if len(res.FailedKeys) != 2 {
		t.Fatalf("FailedKeys len = %d, want 2", len(res.FailedKeys))
	}
}

func TestPerRowScopeSummaryOnlyLeavesKeysEmpty(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(200)},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Int("age").Between(0, 120), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
		o.summaryOnly = true
	})

	if len(res.FailedKeys) != 0 {
		t.Fatalf("FailedKeys = %#v, want empty in summary-only mode", res.FailedKeys)
	}
	if len(db.queries) != 3 {
		t.Fatalf("queries = %d, want 3 without failed-key query", len(db.queries))
	}
}

func TestPerRowScopeFailedKeysCapCompleteCount(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(200)},
		map[string]any{"id": int64(2), "age": int64(300)},
		map[string]any{"id": int64(3), "age": int64(400)},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Int("age").Between(0, 120), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
		o.failedKeysCap = 1
		o.sampleCap = 0
	})

	if res.FailedCount != 3 {
		t.Fatalf("FailedCount = %d, want complete scoped count despite key cap", res.FailedCount)
	}
	if len(res.FailedKeys) != 1 {
		t.Fatalf("FailedKeys len = %d, want 1", len(res.FailedKeys))
	}
	wantPercent := float64(res.FailedCount) / float64(res.Total) * 100
	if res.FailedPercent != wantPercent {
		t.Fatalf("FailedPercent = %v, want %v", res.FailedPercent, wantPercent)
	}
	assertFailedKeyQuery(t, db.queries[2], "id")
	assertScopeQuery(t, db.queries[2], scope, true)
}

func TestPerRowScopeNotEmptyScopedTotals(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "email": "a@b.com"},
		map[string]any{"id": int64(2), "email": ""},
		map[string]any{"id": int64(3), "tenant_id": "t2", "email": ""},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, String("email").NotEmpty(), scope)

	if res.Total != 2 {
		t.Fatalf("Total = %d, want 2 scoped rows", res.Total)
	}
	if res.FailedCount != 1 {
		t.Fatalf("FailedCount = %d, want 1 scoped failure", res.FailedCount)
	}
	if len(db.queries) < 2 {
		t.Fatal("expected scoped count queries")
	}
	assertScopeQuery(t, db.queries[0], scope, false)
	assertScopeQuery(t, db.queries[1], scope, true)
}

func TestPerRowNoScopePreservesUnscopedTotals(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(150)},
		map[string]any{"id": int64(3), "age": int64(10)},
		map[string]any{"id": int64(4), "age": nil},
	))
	db := openHarnessDB(t)

	res, err := Int("age").Between(0, 120).evaluateSQL(context.Background(), db, Table("users"), evalOptions{
		dialect: Postgres(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 4 {
		t.Fatalf("Total = %d, want 4 without scope", res.Total)
	}
	if res.FailedCount != 2 {
		t.Fatalf("FailedCount = %d, want 2 without scope", res.FailedCount)
	}
	if res.FailedPercent != 50 {
		t.Fatalf("FailedPercent = %v, want 50", res.FailedPercent)
	}
}

func TestPerRowScopeUsesSameScopeOnAllQueryKinds(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "status": "active"},
		map[string]any{"id": int64(2), "status": "deleted"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	_ = evalPerRowWithScope(t, db, Column("status").In("active", "pending"), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
		o.sampleCap = 5
	})

	frag := renderedScopeFragment(t, scope)
	for i, q := range db.queries {
		if !strings.Contains(q.text, frag) {
			t.Fatalf("query[%d] missing scope fragment %q: %s", i, frag, q.text)
		}
		if i == 0 {
			continue
		}
		if !strings.Contains(q.text, ") AND (") {
			t.Fatalf("query[%d] missing failure composition: %s", i, q.text)
		}
	}
	if !reflect.DeepEqual(db.queries[1].args[:1], []any{"t1"}) {
		t.Fatalf("failure count args prefix = %#v, want scope value first", db.queries[1].args)
	}
}
