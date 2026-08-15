package gxsql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func reconcileFixtureEqual() map[string][]map[string]any {
	return map[string][]map[string]any{
		"orders": {
			{"id": int64(1), "tenant_id": "t1", "status": "open"},
			{"id": int64(2), "tenant_id": "t1", "status": "closed"},
			{"id": int64(3), "tenant_id": "t2", "status": "open"},
		},
		"orders_served": {
			{"id": int64(10), "tenant_id": "t1", "status": "ready"},
			{"id": int64(11), "tenant_id": "t1", "status": "ready"},
			{"id": int64(12), "tenant_id": "t2", "status": "held"},
		},
	}
}

func reconcileFixtureUnequal() map[string][]map[string]any {
	return map[string][]map[string]any{
		"orders": {
			{"id": int64(1), "tenant_id": "t1"},
			{"id": int64(2), "tenant_id": "t1"},
		},
		"orders_served": {
			{"id": int64(10), "tenant_id": "t1"},
		},
	}
}

func TestReconcileCountsEqual(t *testing.T) {
	setHarnessData(t, reconcileFixtureEqual())
	db := openHarnessDB(t)

	rep, err := NewSuite(ReconcileCounts(Table("orders_served")).Equal()).ValidateTable(
		context.Background(), db, Table("orders"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Kind != KindReconcileCountsEqual {
		t.Fatalf("Kind = %q, want %q", res.Kind, KindReconcileCountsEqual)
	}
	if !res.Success || res.FailedCount != 0 {
		t.Fatalf("got %#v, want equal success", res)
	}
	if res.RowDenominator != RowDenominatorUnavailable {
		t.Fatalf("RowDenominator = %q", res.RowDenominator)
	}
	if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("unexpected samples/keys: %#v %#v", res.SampleValues, res.FailedKeys)
	}
	rf := res.Facts.Reconcile
	if rf == nil {
		t.Fatal("expected Reconcile facts")
	}
	if rf.Left != Table("orders") || rf.Right != Table("orders_served") {
		t.Fatalf("tables = %#v", rf)
	}
	if rf.Relationship != reconcileRelationshipEqual {
		t.Fatalf("Relationship = %q", rf.Relationship)
	}
	if rf.ObservedLeftCount == nil || *rf.ObservedLeftCount != 3 {
		t.Fatalf("ObservedLeftCount = %#v", rf.ObservedLeftCount)
	}
	if rf.ObservedRightCount == nil || *rf.ObservedRightCount != 3 {
		t.Fatalf("ObservedRightCount = %#v", rf.ObservedRightCount)
	}
}

func TestReconcileCountsUnequal(t *testing.T) {
	setHarnessData(t, reconcileFixtureUnequal())
	db := openHarnessDB(t)

	rep, err := NewSuite(ReconcileCounts(Table("orders_served")).Equal()).ValidateTable(
		context.Background(), db, Table("orders"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success || res.FailedCount != 1 {
		t.Fatalf("got %#v, want FailedCount 1", res)
	}
	rf := res.Facts.Reconcile
	if rf == nil || rf.ObservedLeftCount == nil || rf.ObservedRightCount == nil {
		t.Fatalf("dual observations missing: %#v", rf)
	}
	if *rf.ObservedLeftCount != 2 || *rf.ObservedRightCount != 1 {
		t.Fatalf("observations = left %#v right %#v", *rf.ObservedLeftCount, *rf.ObservedRightCount)
	}
}

func TestReconcileCountsBothZeroSuccess(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"orders":        {},
		"orders_served": {},
	})
	db := openHarnessDB(t)

	rep, err := NewSuite(ReconcileCounts(Table("orders_served")).Equal()).ValidateTable(
		context.Background(), db, Table("orders"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.FailedCount != 0 {
		t.Fatalf("got %#v, want both-zero success", res)
	}
	rf := res.Facts.Reconcile
	if rf == nil || rf.ObservedLeftCount == nil || rf.ObservedRightCount == nil {
		t.Fatalf("facts = %#v", rf)
	}
	if *rf.ObservedLeftCount != 0 || *rf.ObservedRightCount != 0 {
		t.Fatalf("observations = %#v %#v", *rf.ObservedLeftCount, *rf.ObservedRightCount)
	}
}

func TestReconcileCountsPrimaryScopeDoesNotAlterSecondary(t *testing.T) {
	setHarnessData(t, reconcileFixtureEqual())
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(ReconcileCounts(Table("orders_served")).Equal()).ValidateTable(
		context.Background(), db, Table("orders"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	// Left scoped to t1 => 2; secondary unscoped => 3.
	if res.Success || res.FailedCount != 1 {
		t.Fatalf("got %#v, want unequal under left-only scope", res)
	}
	rf := res.Facts.Reconcile
	if rf == nil || rf.LeftScopeID != "tenant" {
		t.Fatalf("LeftScopeID = %#v", rf)
	}
	if rf.ObservedLeftCount == nil || *rf.ObservedLeftCount != 2 {
		t.Fatalf("ObservedLeftCount = %#v", rf.ObservedLeftCount)
	}
	if rf.ObservedRightCount == nil || *rf.ObservedRightCount != 3 {
		t.Fatalf("ObservedRightCount = %#v", rf.ObservedRightCount)
	}
	for _, q := range db.queries {
		if strings.Contains(q.text, `"orders_served"`) {
			for _, arg := range q.args {
				if arg == "t1" {
					t.Fatalf("secondary count leaked suite scope args: %#v", q)
				}
			}
			if strings.Contains(q.text, "tenant_id =") {
				t.Fatalf("secondary count leaked suite scope predicate: %s", q.text)
			}
		}
	}
}

func TestReconcileCountsWithSecondaryFilterNarrowsSecondaryOnly(t *testing.T) {
	setHarnessData(t, reconcileFixtureEqual())
	db := openRecordingHarnessDB(t)

	filter := TrustedSecondaryFilter("served-ready", "status = ?", "ready")
	rep, err := NewSuite(
		ReconcileCounts(Table("orders_served")).WithSecondaryFilter(filter).Equal(),
	).ValidateTable(
		context.Background(), db, Table("orders"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	// Left scoped t1 => 2; secondary status=ready => 2.
	if !res.Success || res.FailedCount != 0 {
		t.Fatalf("got %#v, want equal under secondary filter", res)
	}
	rf := res.Facts.Reconcile
	if rf == nil || rf.SecondaryFilterID != "served-ready" || rf.LeftScopeID != "tenant" {
		t.Fatalf("facts = %#v", rf)
	}
	if rf.ObservedLeftCount == nil || *rf.ObservedLeftCount != 2 {
		t.Fatalf("ObservedLeftCount = %#v", rf.ObservedLeftCount)
	}
	if rf.ObservedRightCount == nil || *rf.ObservedRightCount != 2 {
		t.Fatalf("ObservedRightCount = %#v", rf.ObservedRightCount)
	}

	var sawSecondaryFilter, sawLeftScope bool
	for _, q := range db.queries {
		if strings.Contains(q.text, `"orders_served"`) {
			if !strings.Contains(q.text, "status =") {
				t.Fatalf("secondary missing filter predicate: %s", q.text)
			}
			sawSecondaryFilter = true
			if strings.Contains(q.text, "tenant_id =") {
				t.Fatalf("secondary reused left scope: %s", q.text)
			}
		}
		if strings.Contains(q.text, `"orders"`) && !strings.Contains(q.text, `"orders_served"`) {
			if strings.Contains(q.text, "tenant_id =") {
				sawLeftScope = true
			}
			if strings.Contains(q.text, "status =") {
				t.Fatalf("left count reused secondary filter: %s", q.text)
			}
		}
	}
	if !sawSecondaryFilter || !sawLeftScope {
		t.Fatalf("missing expected queries: secondary=%v left=%v queries=%#v", sawSecondaryFilter, sawLeftScope, db.queries)
	}
}

func TestReconcileCountsOneSideFailureContinueOnErrorKeepsOrder(t *testing.T) {
	setHarnessData(t, reconcileFixtureEqual())
	inner := openHarnessDB(t)
	db := &tableMentionFailureDB{
		DB:     inner,
		needle: `"orders_served"`,
		err:    fmt.Errorf("injected secondary count failure"),
	}

	rep, err := NewSuite(
		RowCount().Equal(3),
		ReconcileCounts(Table("orders_served")).Equal(),
		RowCount().Equal(3),
	).ValidateTable(
		context.Background(), db, Table("orders"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(rep.Results))
	}
	if !rep.Results[0].Success || rep.Results[0].Err != nil {
		t.Fatalf("index 0 should pass: %#v", rep.Results[0])
	}
	mid := rep.Results[1]
	if mid.Success || mid.Err == nil {
		t.Fatalf("index 1 should be execution failure: %#v", mid)
	}
	if !errors.Is(mid.Err, ErrCategoryDatabase) {
		t.Fatalf("mid category = %v, want database", mid.Err)
	}
	rf := mid.Facts.Reconcile
	if rf == nil {
		t.Fatal("expected reconcile facts on failed slot")
	}
	if rf.ObservedLeftCount == nil || *rf.ObservedLeftCount != 3 {
		t.Fatalf("ObservedLeftCount = %#v, want observed left only", rf.ObservedLeftCount)
	}
	if rf.ObservedRightCount != nil {
		t.Fatalf("ObservedRightCount = %#v, want nil (no fabricated zero)", rf.ObservedRightCount)
	}
	if !rep.Results[2].Success || rep.Results[2].Err != nil {
		t.Fatalf("index 2 should still run: %#v", rep.Results[2])
	}
}

func TestReconcileCountsLeftSideFailureContinueOnErrorKeepsOrder(t *testing.T) {
	setHarnessData(t, reconcileFixtureEqual())
	inner := openHarnessDB(t)
	db := &oneShotLeftTableFailureDB{
		DB:  inner,
		err: fmt.Errorf("injected left count failure"),
	}

	rep, err := NewSuite(
		ReconcileCounts(Table("orders_served")).Equal(),
		RowCount().Equal(3),
	).ValidateTable(
		context.Background(), db, Table("orders"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(rep.Results))
	}
	leftFail := rep.Results[0]
	if leftFail.Success || leftFail.Err == nil {
		t.Fatalf("index 0 should be left execution failure: %#v", leftFail)
	}
	if !errors.Is(leftFail.Err, ErrCategoryDatabase) {
		t.Fatalf("left category = %v, want database", leftFail.Err)
	}
	rf := leftFail.Facts.Reconcile
	if rf == nil {
		t.Fatal("expected reconcile facts on failed slot")
	}
	if rf.ObservedLeftCount != nil {
		t.Fatalf("ObservedLeftCount = %#v, want nil (no fabricated zero)", rf.ObservedLeftCount)
	}
	if rf.ObservedRightCount != nil {
		t.Fatalf("ObservedRightCount = %#v, want nil (no fabricated zero)", rf.ObservedRightCount)
	}
	if !rep.Results[1].Success || rep.Results[1].Err != nil {
		t.Fatalf("index 1 should still run: %#v", rep.Results[1])
	}
}

func TestReconcileCountsSecondaryFilterArityPreflight(t *testing.T) {
	setHarnessData(t, reconcileFixtureEqual())
	counter := openCountingHarnessDB(t)

	bad := TrustedSecondaryFilter("served-ready", "status = ?", "a", "b")
	_, err := NewSuite(
		ReconcileCounts(Table("orders_served")).WithSecondaryFilter(bad).Equal(),
	).ValidateTable(
		context.Background(), counter, Table("orders"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected secondary filter arity preflight error")
	}
	var pf *PreflightErrors
	if !errors.As(err, &pf) {
		t.Fatalf("err = %v, want PreflightErrors", err)
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v, want invalid_config", err)
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before SQL", counter.queries)
	}
}

func TestReconcileCountsSecondaryTableIdentPreflight(t *testing.T) {
	setHarnessData(t, reconcileFixtureEqual())
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(ReconcileCounts(Table("orders-served")).Equal()).ValidateTable(
		context.Background(), counter, Table("orders"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected invalid secondary identifier preflight error")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v, want invalid_config", err)
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before SQL", counter.queries)
	}
}

func TestReconcileCountsToleranceIneligible(t *testing.T) {
	setHarnessData(t, reconcileFixtureEqual())
	counter := openCountingHarnessDB(t)

	cases := []struct {
		name string
		exp  Expectation
	}{
		{name: "maxFailedCount", exp: WithMaxFailedCount(1, ReconcileCounts(Table("orders_served")).Equal())},
		{name: "maxFailedPercent", exp: WithPolicy(ReconcileCounts(Table("orders_served")).Equal(), Policy{Tolerance: MaxFailedPercent(10)})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counter.queries = 0
			_, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), counter, Table("orders"), WithDialect(Postgres()),
			)
			if err == nil {
				t.Fatal("expected tolerance preflight rejection")
			}
			var pf *PreflightErrors
			if !errors.As(err, &pf) {
				t.Fatalf("got %T, want *PreflightErrors", err)
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) {
				t.Fatalf("category = %v", err)
			}
			if counter.queries != 0 {
				t.Fatalf("queries = %d, want 0", counter.queries)
			}
		})
	}
}

func TestReconcileCountsExportEqualAndUnequalFailedCounts(t *testing.T) {
	cases := []struct {
		name       string
		fixture    map[string][]map[string]any
		wantFailed int
		wantOK     bool
	}{
		{
			name:       "equal",
			fixture:    reconcileFixtureEqual(),
			wantFailed: 0,
			wantOK:     true,
		},
		{
			name:       "unequal",
			fixture:    reconcileFixtureUnequal(),
			wantFailed: 1,
			wantOK:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setHarnessData(t, tc.fixture)
			db := openHarnessDB(t)

			rep, err := NewSuite(ReconcileCounts(Table("orders_served")).Equal()).ValidateTable(
				context.Background(), db, Table("orders"), WithDialect(Postgres()),
			)
			if err != nil {
				t.Fatal(err)
			}
			if got := rep.OK(); got != tc.wantOK {
				t.Fatalf("Report.OK() = %v, want %v", got, tc.wantOK)
			}

			dto, err := ExportReport(rep)
			if err != nil {
				t.Fatal(err)
			}
			if len(dto.Results) != 1 {
				t.Fatalf("len(results) = %d", len(dto.Results))
			}
			out := dto.Results[0]
			if out.Kind != KindReconcileCountsEqual {
				t.Fatalf("Kind = %q", out.Kind)
			}
			counts := out.Counts
			if counts == nil || counts.Failed == nil || *counts.Failed != tc.wantFailed {
				t.Fatalf("counts.failed = %#v, want explicit %d", counts, tc.wantFailed)
			}
			if counts.Total != nil {
				t.Fatalf("counts.total = %#v, want omitted", counts.Total)
			}
			if counts.FailedPercent != nil {
				t.Fatalf("counts.failed_percent = %#v, want omitted", counts.FailedPercent)
			}
			if out.Facts == nil || out.Facts.Reconcile == nil {
				t.Fatalf("reconcile facts missing: %#v", out.Facts)
			}
			rf := out.Facts.Reconcile
			if rf.Left.Table != "orders" || rf.Right.Table != "orders_served" || rf.Relationship != "equal" {
				t.Fatalf("reconcile facts = %#v", rf)
			}
			if rf.ObservedLeftCount == nil || rf.ObservedRightCount == nil {
				t.Fatalf("dual observations missing: %#v", rf)
			}

			data, err := json.Marshal(dto)
			if err != nil {
				t.Fatal(err)
			}
			s := string(data)
			wantFailedJSON := fmt.Sprintf(`"failed":%d`, tc.wantFailed)
			if !strings.Contains(s, wantFailedJSON) {
				t.Fatalf("export JSON missing %s in %s", wantFailedJSON, s)
			}
			for _, forbidden := range []string{
				`"total":`,
				`"failed_percent"`,
				`"samples"`,
				`"failed_keys"`,
				`"diagnostics"`,
				`"args"`,
				"SELECT",
			} {
				if strings.Contains(s, forbidden) {
					t.Fatalf("default export leaked or unexpectedly included %q in %s", forbidden, s)
				}
			}
		})
	}
}

func TestExportReconcileFactsDualSideIdentitiesOmitSQLAndArgs(t *testing.T) {
	left := 2
	right := 2
	rep := Report{
		Target:  &TableRef{Name: "orders"},
		ScopeID: "tenant",
		Results: []Result{{
			Kind:           KindReconcileCountsEqual,
			Success:        true,
			RowDenominator: RowDenominatorUnavailable,
			Facts: ResultFacts{
				Reconcile: &ReconcileFacts{
					Left:               SchemaTable("sales", "orders"),
					Right:              SchemaTable("sales", "orders_served"),
					ObservedLeftCount:  &left,
					ObservedRightCount: &right,
					Relationship:       reconcileRelationshipEqual,
					LeftScopeID:        "tenant",
					SecondaryFilterID:  "served-ready",
				},
			},
			diagnostics: &resultDiagnostics{
				query: `SELECT COUNT(*) FROM "sales"."orders" WHERE tenant_id = $1`,
				args:  []any{"t1-secret"},
			},
		}},
	}

	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	rf := dto.Results[0].Facts.Reconcile
	if rf == nil {
		t.Fatal("expected exported reconcile facts")
	}
	if rf.Left.Schema != "sales" || rf.Left.Table != "orders" {
		t.Fatalf("left = %#v", rf.Left)
	}
	if rf.Right.Schema != "sales" || rf.Right.Table != "orders_served" {
		t.Fatalf("right = %#v", rf.Right)
	}
	if rf.ObservedLeftCount == nil || *rf.ObservedLeftCount != 2 {
		t.Fatalf("ObservedLeftCount = %#v", rf.ObservedLeftCount)
	}
	if rf.ObservedRightCount == nil || *rf.ObservedRightCount != 2 {
		t.Fatalf("ObservedRightCount = %#v", rf.ObservedRightCount)
	}
	if rf.Relationship != "equal" || rf.LeftScopeID != "tenant" || rf.SecondaryFilterID != "served-ready" {
		t.Fatalf("identities = %#v", rf)
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		`"kind":"reconcile_counts_equal"`,
		`"left":{"schema":"sales","table":"orders"}`,
		`"right":{"schema":"sales","table":"orders_served"}`,
		`"observed_left_count":2`,
		`"observed_right_count":2`,
		`"relationship":"equal"`,
		`"left_scope_id":"tenant"`,
		`"secondary_filter_id":"served-ready"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("export JSON missing %s in %s", want, s)
		}
	}
	for _, forbidden := range []string{
		"t1-secret",
		"tenant_id =",
		"status =",
		`"predicate"`,
		"diagnostics",
		`"args"`,
		"samples",
		"failed_keys",
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("default export leaked %q in %s", forbidden, s)
		}
	}
}

type tableMentionFailureDB struct {
	*sql.DB
	needle string
	err    error
}

func (d *tableMentionFailureDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(collapseSpaces(query), d.needle) {
		return nil, d.err
	}
	return d.DB.QueryContext(ctx, query, args...)
}

func (d *tableMentionFailureDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, query, args...)
}

// oneShotLeftTableFailureDB fails the first QueryContext whose SQL targets the
// left table "orders" without matching secondary "orders_served", then
// delegates subsequent calls so later expectations can continue.
type oneShotLeftTableFailureDB struct {
	*sql.DB
	err  error
	used bool
}

func (d *oneShotLeftTableFailureDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q := collapseSpaces(query)
	// Match left FROM "orders" only — never bare `"orders"` (substring of orders_served).
	if !d.used && strings.Contains(q, `FROM "orders"`) && !strings.Contains(q, `"orders_served"`) {
		d.used = true
		return nil, d.err
	}
	return d.DB.QueryContext(ctx, query, args...)
}

func (d *oneShotLeftTableFailureDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, query, args...)
}
