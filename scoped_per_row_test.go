package gxsql

import (
	"context"
	"database/sql"
	"fmt"
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

func countSampleQueries(queries []recordedQuery) int {
	n := 0
	for _, q := range queries {
		if strings.Contains(q.text, " LIMIT ") {
			n++
		}
	}
	return n
}

func countFailedKeyQueries(queries []recordedQuery) int {
	n := 0
	for _, q := range queries {
		upper := strings.ToUpper(q.text)
		if strings.Contains(q.text, " LIMIT ") {
			continue
		}
		if strings.Contains(upper, "SELECT ") && !strings.Contains(upper, "COUNT(") {
			n++
		}
	}
	return n
}

func TestSharedScalarEvaluationOneCombinedCountStatement(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(200), "email": ""},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(10), "email": ""},
	))
	scope := TrustedScope("tenant", "tenant_id = ?", "t1")

	t.Run("compatibleOnly", func(t *testing.T) {
		suite := NewSuite(
			Int("age").Between(0, 120),
			String("email").NotEmpty(),
		)
		seq := openCountingHarnessDB(t)
		if _, err := suite.ValidateTable(
			context.Background(), seq, Table("users"),
			WithDialect(Postgres()), WithScope(scope), SummaryOnly(), WithSampleCap(0),
		); err != nil {
			t.Fatalf("sequential ValidateTable error = %v", err)
		}
		if seq.queries != 3 {
			t.Fatalf("sequential queries = %d, want 3 (1 total + 2 failure counts)", seq.queries)
		}

		shared := openCountingHarnessDB(t)
		if _, err := suite.ValidateTable(
			context.Background(), shared, Table("users"),
			WithDialect(Postgres()), WithScope(scope), SummaryOnly(), WithSampleCap(0),
			WithSharedScalarEvaluation(),
		); err != nil {
			t.Fatalf("shared ValidateTable error = %v", err)
		}
		if shared.queries != 2 {
			t.Fatalf("shared queries = %d, want 2 (1 total + 1 combined count)", shared.queries)
		}
	})

	t.Run("nonContiguousCompatibleStaySequential", func(t *testing.T) {
		suite := NewSuite(
			Int("age").Between(0, 120),
			RowCount().Equal(2),
			String("email").NotEmpty(),
		)
		seq := openCountingHarnessDB(t)
		if _, err := suite.ValidateTable(
			context.Background(), seq, Table("users"),
			WithDialect(Postgres()), WithScope(scope), SummaryOnly(), WithSampleCap(0),
		); err != nil {
			t.Fatalf("sequential ValidateTable error = %v", err)
		}
		if seq.queries != 4 {
			t.Fatalf("sequential queries = %d, want 4 (1 total + 2 failure counts + 1 row count)", seq.queries)
		}

		shared := openCountingHarnessDB(t)
		if _, err := suite.ValidateTable(
			context.Background(), shared, Table("users"),
			WithDialect(Postgres()), WithScope(scope), SummaryOnly(), WithSampleCap(0),
			WithSharedScalarEvaluation(),
		); err != nil {
			t.Fatalf("shared ValidateTable error = %v", err)
		}
		// Compatible slots separated by RowCount are not combined.
		if shared.queries != seq.queries {
			t.Fatalf("shared queries = %d, want sequential parity %d", shared.queries, seq.queries)
		}
	})

	t.Run("contiguousCompatibleCombineBeforeIncompatible", func(t *testing.T) {
		suite := NewSuite(
			Int("age").Between(0, 120),
			String("email").NotEmpty(),
			RowCount().Equal(2),
		)
		shared := openCountingHarnessDB(t)
		if _, err := suite.ValidateTable(
			context.Background(), shared, Table("users"),
			WithDialect(Postgres()), WithScope(scope), SummaryOnly(), WithSampleCap(0),
			WithSharedScalarEvaluation(),
		); err != nil {
			t.Fatalf("shared ValidateTable error = %v", err)
		}
		if shared.queries != 3 {
			t.Fatalf("shared queries = %d, want 3 (1 total + 1 combined count + 1 row count)", shared.queries)
		}
	})
}

func TestSharedScalarEvaluationZeroFailureSkipsDiagnostics(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(200), "email": ""},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
		WithKey("id"),
		WithSampleCap(5),
		WithSharedScalarEvaluation(),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	for i, res := range rep.Results {
		if res.FailedCount != 0 {
			t.Fatalf("result[%d] FailedCount = %d, want 0", i, res.FailedCount)
		}
		if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
			t.Fatalf("result[%d] diagnostics = samples %#v keys %#v, want empty", i, res.SampleValues, res.FailedKeys)
		}
	}
	if len(db.queries) != 2 {
		t.Fatalf("queries = %d, want 2 (scoped total + combined count; skip diagnostics)", len(db.queries))
	}
	assertNoSampleOrFailedKeyQueries(t, db.queries)
	if got := len(scopedDenominatorTotals(db.queries)); got != 1 {
		t.Fatalf("scoped denominator totals = %d, want 1", got)
	}
}

func TestSharedScalarEvaluationIndependentSampleAndKeyDiagnostics(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": ""},
		map[string]any{"id": int64(2), "age": int64(200), "email": "ok@x.com"},
		map[string]any{"id": int64(3), "age": int64(30), "email": "ok2@x.com"},
		map[string]any{"id": int64(4), "tenant_id": "t2", "age": int64(999), "email": ""},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		WithID("age-check", Int("age").Between(0, 120)),
		WithID("email-check", String("email").NotEmpty()),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
		WithKey("id"),
		WithFailedKeysCap(0),
		WithSampleCap(5),
		WithSharedScalarEvaluation(),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(rep.Results))
	}
	age, email := rep.Results[0], rep.Results[1]
	if age.FailedCount != 1 || email.FailedCount != 1 {
		t.Fatalf("FailedCount age=%d email=%d, want 1 and 1", age.FailedCount, email.FailedCount)
	}
	if !reflect.DeepEqual(age.SampleValues, []any{int64(200)}) {
		t.Fatalf("age SampleValues = %#v, want []any{int64(200)}", age.SampleValues)
	}
	if !reflect.DeepEqual(email.SampleValues, []any{""}) {
		t.Fatalf("email SampleValues = %#v, want one empty string", email.SampleValues)
	}
	if !reflect.DeepEqual(age.FailedKeys, []RowKey{{int64(2)}}) {
		t.Fatalf("age FailedKeys = %#v, want []RowKey{{2}}", age.FailedKeys)
	}
	if !reflect.DeepEqual(email.FailedKeys, []RowKey{{int64(1)}}) {
		t.Fatalf("email FailedKeys = %#v, want []RowKey{{1}}", email.FailedKeys)
	}
	if reflect.DeepEqual(age.SampleValues, email.SampleValues) {
		t.Fatal("sample diagnostics must remain independent per result")
	}
	if reflect.DeepEqual(age.FailedKeys, email.FailedKeys) {
		t.Fatal("failed-key diagnostics must remain independent per result")
	}

	if got := countSampleQueries(db.queries); got != 2 {
		t.Fatalf("sample queries = %d, want 2 (one per nonzero result)", got)
	}
	if got := countFailedKeyQueries(db.queries); got != 2 {
		t.Fatalf("failed-key queries = %d, want 2 (one per nonzero result)", got)
	}
	if len(db.queries) != 6 {
		t.Fatalf("queries = %d, want 6 (total + combined count + 2 samples + 2 keys)", len(db.queries))
	}
	assertSampleQuery(t, db.queries[2], "age")
	assertFailedKeyQuery(t, db.queries[3], "id")
	assertSampleQuery(t, db.queries[4], "email")
	assertFailedKeyQuery(t, db.queries[5], "id")
}

func isSharedScalarCountQuery(text string) bool {
	q := collapseSpaces(text)
	upper := strings.ToUpper(q)
	if isScopedDenominatorTotalQuery(q) {
		return false
	}
	if strings.Contains(upper, "SELECT COUNT(*)") && strings.Contains(q, ") AND (") {
		// Classic sequential per-expectation failure count.
		return false
	}
	agg := strings.Count(upper, "COUNT(") + strings.Count(upper, "SUM(")
	if agg >= 2 {
		return true
	}
	if (strings.Contains(upper, "FILTER") || strings.Contains(upper, " CASE ")) && agg >= 1 {
		return true
	}
	from := strings.Index(upper, " FROM ")
	if from > 0 && strings.Contains(upper[:from], ",") && agg >= 1 {
		return true
	}
	return false
}

func sharedScalarCountQueries(queries []recordedQuery) []recordedQuery {
	var out []recordedQuery
	for _, q := range queries {
		if isSharedScalarCountQuery(q.text) {
			out = append(out, q)
		}
	}
	return out
}

func assertSharedScalarNoSequentialFallback(t *testing.T, queries []recordedQuery) {
	t.Helper()
	shared := sharedScalarCountQueries(queries)
	if len(shared) == 0 {
		t.Fatal("expected a shared scalar count statement before asserting no sequential fallback")
	}
	sharedSeen := 0
	for _, q := range queries {
		if isSharedScalarCountQuery(q.text) {
			sharedSeen++
			continue
		}
		if sharedSeen == 0 {
			continue
		}
		if strings.Contains(strings.ToUpper(q.text), "SELECT COUNT(*)") && strings.Contains(q.text, ") AND (") {
			t.Fatalf("sequential failure-count fallback after shared statement: %s", q.text)
		}
	}
	if sharedSeen != 1 {
		t.Fatalf("shared scalar count statements = %d, want exactly 1 (no retry)", sharedSeen)
	}
}

type sharedScalarErrorDB struct {
	DB
	queries  []recordedQuery
	err      error
	scanDB   DB
	failHits int
}

func (s *sharedScalarErrorDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	text := collapseSpaces(query)
	s.queries = append(s.queries, recordedQuery{
		text: text,
		args: append([]any(nil), args...),
	})
	if isSharedScalarCountQuery(text) {
		s.failHits++
		if s.failHits == 1 {
			if s.scanDB != nil {
				return s.scanDB.QueryContext(ctx, query, args...)
			}
			return nil, s.err
		}
	}
	return s.DB.QueryContext(ctx, query, args...)
}

func (s *sharedScalarErrorDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	text := collapseSpaces(query)
	s.queries = append(s.queries, recordedQuery{
		text: text,
		args: append([]any(nil), args...),
	})
	return s.DB.QueryRowContext(ctx, query, args...)
}

func openSharedScalarErrorHarnessDB(t *testing.T, err error, scanMode bool) *sharedScalarErrorDB {
	t.Helper()
	db := &sharedScalarErrorDB{
		DB:  openHarnessDB(t),
		err: err,
	}
	if scanMode {
		db.scanDB = openScanErrorDB(t)
	}
	return db
}

func TestSharedScalarEvaluationCapturedDiagnosticsContainSharedSQLAndArgs(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com", "status": "active"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com", "status": "pending"},
	))
	scope := TrustedScope("tenant", "tenant_id = ?", "t1")
	injected := fmt.Errorf("injected shared scalar database failure")
	db := openSharedScalarErrorHarnessDB(t, injected, false)

	rep, err := NewSuite(
		WithID("age-check", Int("age").Between(0, 120)),
		WithID("status-check", Column("status").In("active", "pending")),
		WithID("rows", RowCount().Equal(2)),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
		SummaryOnly(),
		ContinueOnError(),
		CaptureQueryDiagnostics(),
		WithSharedScalarEvaluation(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results len = %d, want 3", len(rep.Results))
	}

	shared := sharedScalarCountQueries(db.queries)
	if len(shared) != 1 {
		t.Fatalf("shared scalar statements = %d, want 1; queries=%#v", len(shared), db.queries)
	}
	wantQuery, wantArgs := shared[0].text, shared[0].args

	for i, res := range rep.Results[:2] {
		if res.Err == nil {
			t.Fatalf("compatible result[%d] missing shared error", i)
		}
		if res.diagnostics == nil {
			t.Fatalf("compatible result[%d] missing captured diagnostics", i)
		}
		gotQuery := collapseSpaces(res.diagnostics.query)
		if gotQuery != wantQuery {
			t.Fatalf("compatible result[%d] diagnostics query = %q, want actual shared SQL %q", i, gotQuery, wantQuery)
		}
		if !reflect.DeepEqual(res.diagnostics.args, wantArgs) {
			t.Fatalf("compatible result[%d] diagnostics args = %#v, want exact shared args %#v", i, res.diagnostics.args, wantArgs)
		}
	}

	rows := rep.Results[2]
	if rows.Err != nil || !rows.Success {
		t.Fatalf("unaffected RowCount = %#v, want independent success", rows)
	}
	if rows.diagnostics != nil {
		gotQuery := collapseSpaces(rows.diagnostics.query)
		if gotQuery == wantQuery {
			t.Fatalf("unaffected slot must not claim the shared SQL: %q", gotQuery)
		}
		if strings.Contains(gotQuery, ") AND (") {
			t.Fatalf("unaffected RowCount diagnostics look like a sequential failure count: %q", gotQuery)
		}
	}
	assertSharedScalarNoSequentialFallback(t, db.queries)
}

func TestSharedScalarEvaluationBoundArgsScopeFirstThenPredicateOrderAcrossDialects(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "status": "active"},
		map[string]any{"id": int64(2), "age": int64(30), "status": "pending"},
	))
	scope := TrustedScope("tenant", "tenant_id = ?", "t1")
	positionalArgs := []any{"t1", 0, 120, "active", "pending"}
	questionArgs := []any{0, 120, "active", "pending", "t1"}

	dialects := []struct {
		name       string
		dialect    Dialect
		positional bool
	}{
		{name: "postgres", dialect: Postgres(), positional: true},
		{name: "duckdb", dialect: DuckDB(), positional: true},
		{name: "sqlite", dialect: SQLite(), positional: false},
		{name: "mysql", dialect: MySQL(), positional: false},
	}

	for _, tc := range dialects {
		t.Run(tc.name, func(t *testing.T) {
			injected := fmt.Errorf("injected shared scalar database failure (%s)", tc.name)
			db := openSharedScalarErrorHarnessDB(t, injected, false)
			wantArgs := positionalArgs
			if !tc.positional {
				wantArgs = questionArgs
			}

			rep, err := NewSuite(
				Int("age").Between(0, 120),
				Column("status").In("active", "pending"),
			).ValidateTable(
				context.Background(), db, Table("users"),
				WithDialect(tc.dialect),
				WithScope(scope),
				SummaryOnly(),
				ContinueOnError(),
				CaptureQueryDiagnostics(),
				WithSharedScalarEvaluation(),
			)
			if err != nil {
				t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
			}
			if len(rep.Results) != 2 {
				t.Fatalf("results len = %d, want 2", len(rep.Results))
			}

			shared := sharedScalarCountQueries(db.queries)
			if len(shared) != 1 {
				t.Fatalf("shared scalar statements = %d, want 1; queries=%#v", len(shared), db.queries)
			}
			if !reflect.DeepEqual(shared[0].args, wantArgs) {
				t.Fatalf("recorded shared args = %#v, want SQL appearance order %#v", shared[0].args, wantArgs)
			}

			for i, res := range rep.Results {
				if res.diagnostics == nil {
					t.Fatalf("result[%d] missing captured diagnostics", i)
				}
				if !reflect.DeepEqual(res.diagnostics.args, wantArgs) {
					t.Fatalf("result[%d] diagnostics args = %#v, want %#v", i, res.diagnostics.args, wantArgs)
				}
				query := res.diagnostics.query
				if tc.positional {
					if !strings.Contains(query, "$1") || strings.Contains(query, "?") {
						t.Fatalf("result[%d] query placeholder style = %q, want $n for %s", i, query, tc.name)
					}
				} else if strings.Contains(query, "$") || !strings.Contains(query, "?") {
					t.Fatalf("result[%d] query placeholder style = %q, want ? for %s", i, query, tc.name)
				}
			}
			assertSharedScalarNoSequentialFallback(t, db.queries)
		})
	}
}

func TestSharedScalarEvaluationQuestionMarkSemanticParity(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(200), "email": ""},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(10), "email": ""},
	))
	suite := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
	)
	optsBase := []Option{
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
		WithKey("id"),
		WithSampleCap(5),
	}
	for _, dialect := range []Dialect{SQLite(), MySQL()} {
		t.Run(fmt.Sprintf("%T", dialect), func(t *testing.T) {
			opts := append([]Option{WithDialect(dialect)}, optsBase...)
			seq, err := suite.ValidateTable(context.Background(), openHarnessDB(t), Table("users"), opts...)
			if err != nil {
				t.Fatalf("sequential ValidateTable error = %v", err)
			}
			shared, err := suite.ValidateTable(
				context.Background(), openHarnessDB(t), Table("users"),
				append(opts, WithSharedScalarEvaluation())...,
			)
			if err != nil {
				t.Fatalf("shared ValidateTable error = %v", err)
			}
			assertSemanticReportParity(t, shared, seq)
		})
	}
}

type absentColumnErrorDB struct {
	DB
	queries []recordedQuery
}

func (a *absentColumnErrorDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	text := collapseSpaces(query)
	a.queries = append(a.queries, recordedQuery{
		text: text,
		args: append([]any(nil), args...),
	})
	if strings.Contains(text, "absent_column") {
		return nil, fmt.Errorf("injected absent_column database failure")
	}
	return a.DB.QueryContext(ctx, query, args...)
}

func (a *absentColumnErrorDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	text := collapseSpaces(query)
	a.queries = append(a.queries, recordedQuery{
		text: text,
		args: append([]any(nil), args...),
	})
	return a.DB.QueryRowContext(ctx, query, args...)
}

func TestSharedScalarEvaluationPreservesDeclarationOrderAroundIncompatible(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
	))
	db := &absentColumnErrorDB{DB: openHarnessDB(t)}
	rep, err := NewSuite(
		WithID("age-check", Int("age").Between(0, 120)),
		WithID("rows", RowCount().Equal(2)),
		WithID("absent", Int("absent_column").Between(0, 1)),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
		SummaryOnly(),
		ContinueOnError(),
		WithSharedScalarEvaluation(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results len = %d, want 3", len(rep.Results))
	}
	if rep.Results[0].Err != nil || !rep.Results[0].Success || rep.Results[0].ID != "age-check" {
		t.Fatalf("age result = %#v, want independent success", rep.Results[0])
	}
	if rep.Results[1].Err != nil || !rep.Results[1].Success || rep.Results[1].ID != "rows" {
		t.Fatalf("RowCount result = %#v, want independent success before absent-column error", rep.Results[1])
	}
	if rep.Results[2].Err == nil || rep.Results[2].Success {
		t.Fatalf("absent result = %#v, want execution error", rep.Results[2])
	}
	firstAbsent := -1
	for i, q := range db.queries {
		if strings.Contains(q.text, "absent_column") {
			firstAbsent = i
			break
		}
	}
	// Contiguous batching must not pull absent_column into the first shared/age
	// statement ahead of the intervening RowCount expectation.
	if firstAbsent < 2 {
		t.Fatalf("absent_column referenced too early at query %d; queries=%#v", firstAbsent, db.queries)
	}
}

type sharedScalarSampleErrorDB struct {
	DB
	queries   []recordedQuery
	failLimit bool
	failHits  int
}

func (s *sharedScalarSampleErrorDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	text := collapseSpaces(query)
	s.queries = append(s.queries, recordedQuery{
		text: text,
		args: append([]any(nil), args...),
	})
	if s.failLimit && strings.Contains(text, " LIMIT ") {
		s.failHits++
		return nil, fmt.Errorf("injected sample diagnostic failure")
	}
	return s.DB.QueryContext(ctx, query, args...)
}

func (s *sharedScalarSampleErrorDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	text := collapseSpaces(query)
	s.queries = append(s.queries, recordedQuery{
		text: text,
		args: append([]any(nil), args...),
	})
	return s.DB.QueryRowContext(ctx, query, args...)
}

func TestSharedScalarEvaluationDiagnosticErrorAbortsLaterSamplesWithoutContinueOnError(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(200), "email": ""},
		map[string]any{"id": int64(2), "age": int64(250), "email": ""},
	))
	db := &sharedScalarSampleErrorDB{DB: openHarnessDB(t), failLimit: true}
	_, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
		WithSampleCap(5),
		SummaryOnly(),
		WithSharedScalarEvaluation(),
	)
	if err == nil {
		t.Fatal("expected sample diagnostic error")
	}
	sampleQueries := 0
	for _, q := range db.queries {
		if strings.Contains(q.text, " LIMIT ") {
			sampleQueries++
		}
	}
	if sampleQueries != 1 {
		t.Fatalf("sample queries = %d, want 1 (abort after first diagnostic error); queries=%#v", sampleQueries, db.queries)
	}
}

func TestSharedScalarEvaluationDiagnosticErrorClearsTolerance(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(200), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
	))
	db := &sharedScalarSampleErrorDB{DB: openHarnessDB(t), failLimit: true}
	rep, err := NewSuite(
		WithMaxFailedCount(1, Int("age").Between(0, 120)),
		WithMaxFailedCount(1, String("email").NotEmpty()),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
		WithSampleCap(5),
		SummaryOnly(),
		ContinueOnError(),
		WithSharedScalarEvaluation(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
	}
	age := rep.Results[0]
	if age.Err == nil {
		t.Fatalf("age result missing diagnostic error: %#v", age)
	}
	if age.Success || age.Tolerated {
		t.Fatalf("age Success=%v Tolerated=%v, want false/false on error", age.Success, age.Tolerated)
	}
}

func TestSharedScalarEvaluationChunksLargeContiguousRuns(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com", "status": "active"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com", "status": "pending"},
	))
	old := sharedScalarMaxSelectTargets
	sharedScalarMaxSelectTargets = 2
	t.Cleanup(func() { sharedScalarMaxSelectTargets = old })

	exps := []Expectation{
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
		Column("status").In("active", "pending"),
		Column("id").NotNull(),
		Int("age").GreaterOrEqual(0),
	}
	db := openRecordingHarnessDB(t)
	if _, err := NewSuite(exps...).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
		SummaryOnly(),
		WithSampleCap(0),
		WithSharedScalarEvaluation(),
	); err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	caseQueries := 0
	for _, q := range db.queries {
		if strings.Contains(strings.ToUpper(q.text), "COUNT(CASE") {
			caseQueries++
		}
	}
	if caseQueries != 3 {
		t.Fatalf("COUNT(CASE chunks = %d, want 3 for 5 targets with max=2; queries=%#v", caseQueries, db.queries)
	}
}
