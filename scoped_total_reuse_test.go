package gxsql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func isScopedDenominatorTotalQuery(text string) bool {
	upper := strings.ToUpper(text)
	if !strings.Contains(upper, "SELECT COUNT(*)") {
		return false
	}
	return !strings.Contains(text, ") AND (")
}

func scopedDenominatorTotals(queries []recordedQuery) []recordedQuery {
	var out []recordedQuery
	for _, q := range queries {
		if isScopedDenominatorTotalQuery(q.text) {
			out = append(out, q)
		}
	}
	return out
}

func countStarQueries(queries []recordedQuery) int {
	n := 0
	for _, q := range queries {
		if strings.Contains(strings.ToUpper(q.text), "SELECT COUNT(*)") {
			n++
		}
	}
	return n
}

func countFailureCountQueries(queries []recordedQuery) int {
	n := 0
	for _, q := range queries {
		if strings.Contains(strings.ToUpper(q.text), "SELECT COUNT(*)") && strings.Contains(q.text, ") AND (") {
			n++
		}
	}
	return n
}

func assertIdenticalRecordedQueries(t *testing.T, queries []recordedQuery) {
	t.Helper()
	if len(queries) == 0 {
		t.Fatal("expected at least one query")
	}
	want := queries[0]
	for i, q := range queries[1:] {
		if q.text != want.text {
			t.Fatalf("query[%d] text = %q, want %q", i+1, q.text, want.text)
		}
		if !reflect.DeepEqual(q.args, want.args) {
			t.Fatalf("query[%d] args = %#v, want %#v", i+1, q.args, want.args)
		}
	}
}

func assertNoSampleOrFailedKeyQueries(t *testing.T, queries []recordedQuery) {
	t.Helper()
	for i, q := range queries {
		if strings.Contains(q.text, " LIMIT ") {
			t.Fatalf("query[%d] is a sample query: %s", i, q.text)
		}
		upper := strings.ToUpper(q.text)
		if strings.Contains(upper, "SELECT ") && !strings.Contains(upper, "COUNT(") {
			t.Fatalf("query[%d] is a failed-key query: %s", i, q.text)
		}
	}
}

func stripResultDiagnostics(r Result) Result {
	r.diagnostics = nil
	return r
}

func evalExpectationsIndividually(
	t *testing.T,
	expectations []Expectation,
	db DB,
	scope trustedScope,
	opts evalOptions,
) []Result {
	t.Helper()
	results := make([]Result, len(expectations))
	for i, exp := range expectations {
		res, err := exp.evaluateSQL(context.Background(), db, Table("users"), opts)
		if err != nil {
			t.Fatalf("evaluateSQL[%d]: %v", i, err)
		}
		if res.Kind == "" {
			res.Kind = expectationKind(exp)
		}
		if id := expectationID(exp); id != "" && res.ID == "" {
			res.ID = id
		}
		results[i] = stripResultDiagnostics(res)
	}
	return results
}

type firstScopedTotalFailureDB struct {
	DB
	queries    []recordedQuery
	err        error
	failTotals int
}

func (f *firstScopedTotalFailureDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	text := collapseSpaces(query)
	f.queries = append(f.queries, recordedQuery{
		text: text,
		args: append([]any(nil), args...),
	})
	if isScopedDenominatorTotalQuery(text) {
		f.failTotals++
		if f.failTotals == 1 {
			return nil, f.err
		}
	}
	return f.DB.QueryContext(ctx, query, args...)
}

func openFirstScopedTotalFailureHarnessDB(t *testing.T, err error) *firstScopedTotalFailureDB {
	t.Helper()
	return &firstScopedTotalFailureDB{
		DB:  openHarnessDB(t),
		err: err,
	}
}

func TestScopedTotalReuseMultiChecksOneSharedDenominatorTotal(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(200), "email": ""},
		map[string]any{"id": int64(3), "age": int64(30), "email": "dup"},
		map[string]any{"id": int64(4), "age": int64(40), "email": "dup"},
		map[string]any{"id": int64(5), "tenant_id": "t2", "age": int64(10), "email": "dup"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
		Column("email").Unique(),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results len = %d, want 3", len(rep.Results))
	}

	totals := scopedDenominatorTotals(db.queries)
	if len(totals) != 1 {
		t.Fatalf("scoped denominator totals = %d, want exactly 1 shared total", len(totals))
	}
	assertScopeQuery(t, totals[0], scope, false)

	if got := countFailureCountQueries(db.queries); got != 3 {
		t.Fatalf("failure count queries = %d, want 3 (one per expectation)", got)
	}

	wantFailed := []int{1, 1, 2}
	for i, res := range rep.Results {
		if res.Total != 4 {
			t.Fatalf("result[%d] Total = %d, want 4 scoped rows", i, res.Total)
		}
		if res.FailedCount != wantFailed[i] {
			t.Fatalf("result[%d] FailedCount = %d, want %d", i, res.FailedCount, wantFailed[i])
		}
		wantPercent := float64(res.FailedCount) / float64(res.Total) * 100
		if res.FailedPercent != wantPercent {
			t.Fatalf("result[%d] FailedPercent = %v, want %v", i, res.FailedPercent, wantPercent)
		}
	}
}

func TestScopedTotalObservationOnlyNoExtraDenominatorTotal(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "amount": float64(10), "status": "active"},
		map[string]any{"id": int64(2), "amount": float64(20), "status": "active"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "amount": float64(100), "status": "deleted"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")

	t.Run("rowCountOnly", func(t *testing.T) {
		db := openRecordingHarnessDB(t)
		_, err := NewSuite(RowCount().Equal(2)).ValidateTable(
			context.Background(), db, Table("users"),
			WithDialect(Postgres()),
			WithScope(scope),
		)
		if err != nil {
			t.Fatalf("ValidateTable error = %v", err)
		}
		if got := countStarQueries(db.queries); got != 1 {
			t.Fatalf("COUNT(*) queries = %d, want 1 observation query only", got)
		}
	})

	t.Run("rowCountDistinctCountAggregate", func(t *testing.T) {
		db := openRecordingHarnessDB(t)
		_, err := NewSuite(
			RowCount().Equal(2),
			Column("status").DistinctCount().Equal(1),
			Float("amount").AverageBetween(10, 20),
		).ValidateTable(
			context.Background(), db, Table("users"),
			WithDialect(Postgres()),
			WithScope(scope),
		)
		if err != nil {
			t.Fatalf("ValidateTable error = %v", err)
		}
		if got := countStarQueries(db.queries); got != 1 {
			t.Fatalf("COUNT(*) queries = %d, want 1 for RowCount observation only", got)
		}
		for _, q := range db.queries {
			upper := strings.ToUpper(q.text)
			if strings.Contains(upper, "COUNT(DISTINCT") {
				continue
			}
			if strings.Contains(upper, "AVG(") {
				continue
			}
			if !strings.Contains(upper, "SELECT COUNT(*)") {
				t.Fatalf("unexpected query shape: %s", q.text)
			}
		}
	})
}

func TestTotalReuseReportParityWithPerExpectationBaseline(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(10), "email": "c@c.com"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	expectations := []Expectation{
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
		Column("email").Unique(),
	}

	harnessDB := openHarnessDB(t)
	evalOpts := evalOptions{
		dialect:       Postgres(),
		sampleCap:     0,
		failedKeysCap: DefaultFailedKeysCap,
		summaryOnly:   true,
		scope:         &scope,
	}
	baseline := evalExpectationsIndividually(t, expectations, harnessDB, scope, evalOpts)

	recordingDB := openRecordingHarnessDB(t)
	rep, err := NewSuite(expectations...).ValidateTable(
		context.Background(), recordingDB, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	if len(rep.Results) != len(baseline) {
		t.Fatalf("results len = %d, want %d", len(rep.Results), len(baseline))
	}
	got := make([]Result, len(rep.Results))
	for i, res := range rep.Results {
		got[i] = stripResultDiagnostics(res)
	}
	if !reflect.DeepEqual(got, baseline) {
		t.Fatalf("reused report != per-expectation baseline\ngot:  %#v\nwant: %#v", got, baseline)
	}
}

func TestScopedTotalReusePassingRunSkipsSampleAndFailedKeyQueries(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
		Column("email").Unique(),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
		WithKey("id"),
		WithSampleCap(5),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	for i, res := range rep.Results {
		if !res.Success {
			t.Fatalf("result[%d] should pass: %#v", i, res)
		}
		if res.FailedCount != 0 {
			t.Fatalf("result[%d] FailedCount = %d, want 0", i, res.FailedCount)
		}
		if len(res.SampleValues) != 0 {
			t.Fatalf("result[%d] SampleValues = %#v, want empty", i, res.SampleValues)
		}
		if len(res.FailedKeys) != 0 {
			t.Fatalf("result[%d] FailedKeys = %#v, want empty", i, res.FailedKeys)
		}
	}
	assertNoSampleOrFailedKeyQueries(t, db.queries)
}

func TestTotalReuseDefaultSharedTotalFailureReturnsZeroReport(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openFirstScopedTotalFailureHarnessDB(t, fmt.Errorf("injected shared total failure"))

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
	)
	if err == nil {
		t.Fatal("expected shared total failure error")
	}
	if !errors.Is(err, ErrCategoryDatabase) {
		t.Fatalf("error category = %v, want database", err)
	}
	if len(rep.Results) != 0 || rep.Target != nil || rep.ScopeID != "" {
		t.Fatalf("report = %#v, want zero report", rep)
	}
	if len(scopedDenominatorTotals(db.queries)) != 1 {
		t.Fatalf("scoped denominator total queries = %d, want 1 before abort", len(scopedDenominatorTotals(db.queries)))
	}
}

func TestScopedTotalReuseContinueOnErrorDependencyAndIndependentSlots(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com", "amount": float64(10)},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com", "amount": float64(20)},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(10), "email": "c@c.com", "amount": float64(100)},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	injected := fmt.Errorf("injected shared total failure")
	db := openFirstScopedTotalFailureHarnessDB(t, injected)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
		RowCount().Equal(2),
		Float("amount").AverageBetween(10, 20),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
		ContinueOnError(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
	}
	if len(rep.Results) != 4 {
		t.Fatalf("results len = %d, want 4", len(rep.Results))
	}

	dep0, dep1 := rep.Results[0], rep.Results[1]
	if dep0.Err == nil || dep1.Err == nil {
		t.Fatalf("dependent results must record shared total failure: %#v, %#v", dep0, dep1)
	}
	if !errors.Is(dep0.Err, ErrCategoryDatabase) || !errors.Is(dep1.Err, ErrCategoryDatabase) {
		t.Fatalf("dependent errors = %v and %v, want database category", dep0.Err, dep1.Err)
	}
	if dep0.Err.Error() != dep1.Err.Error() {
		t.Fatalf("dependent errors differ:\n[%d] %v\n[%d] %v", 0, dep0.Err, 1, dep1.Err)
	}
	if dep0.Success || dep1.Success {
		t.Fatal("dependent slots must not succeed when shared total fails")
	}
	if dep0.Total != 0 || dep1.Total != 0 {
		t.Fatalf("dependent totals = (%d, %d), want 0 when shared total unavailable", dep0.Total, dep1.Total)
	}

	rowCount := rep.Results[2]
	if rowCount.Err != nil {
		t.Fatalf("RowCount should evaluate independently: %v", rowCount.Err)
	}
	if !rowCount.Success {
		t.Fatalf("RowCount result = %#v, want success", rowCount)
	}
	if rowCount.Facts.ObservedCount == nil || *rowCount.Facts.ObservedCount != 2 {
		t.Fatalf("RowCount observed = %v, want 2", rowCount.Facts.ObservedCount)
	}

	aggregate := rep.Results[3]
	if aggregate.Err != nil {
		t.Fatalf("aggregate should evaluate independently: %v", aggregate.Err)
	}
	if !aggregate.Success {
		t.Fatalf("aggregate result = %#v, want success", aggregate)
	}
	if aggregate.Facts.ObservedFloat == nil || *aggregate.Facts.ObservedFloat != 15 {
		t.Fatalf("aggregate observed = %v, want 15", aggregate.Facts.ObservedFloat)
	}

	if len(scopedDenominatorTotals(db.queries)) != 2 {
		t.Fatalf("scoped denominator totals = %d, want 1 failed shared total plus 1 RowCount observation", len(scopedDenominatorTotals(db.queries)))
	}
	assertIdenticalRecordedQueries(t, scopedDenominatorTotals(db.queries))
}
