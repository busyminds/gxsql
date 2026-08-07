package gxsql

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestValidateTableCollectAllOrderedResults(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(200), "email": ""},
	))
	db := openHarnessDB(t)

	suite := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
		RowCount().Equal(2),
	)
	rep, err := suite.ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatalf("ValidateTable error: %v", err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results len = %d, want 3", len(rep.Results))
	}
	if rep.Results[0].Name != "age between [0,120]" {
		t.Fatalf("first result name = %q", rep.Results[0].Name)
	}
	if rep.Results[1].Success {
		t.Fatal("second expectation should fail")
	}
	if !rep.Results[2].Success {
		t.Fatal("third expectation should pass on count")
	}
	if rep.OK() {
		t.Fatal("report should not be OK")
	}
}

func TestValidateTableValidationFailureNotReturnedAsError(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(200)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("execution error = %v, want nil", err)
	}
	if rep.Err() == nil {
		t.Fatal("expected validation failure via report.Err()")
	}
}

func TestNilExpectationPreflightErrorByDefault(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1)},
	))
	db := openHarnessDB(t)

	_, err := NewSuite(nil).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected preflight configuration error")
	}
	var pf *PreflightErrors
	if !errors.As(err, &pf) {
		t.Fatalf("got %T, want *PreflightErrors", err)
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatal("expected invalid_config category")
	}
}

func TestNilExpectationMarkedAsFailureWithContinueOnError(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(nil).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 1 {
		t.Fatalf("results len = %d", len(rep.Results))
	}
	if rep.Results[0].Success {
		t.Fatal("nil expectation should fail")
	}
	if rep.Results[0].Err == nil {
		t.Fatal("nil expectation should set Result.Err")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryInvalidConfig) {
		t.Fatalf("Err category = %v", rep.Results[0].Err)
	}
}

func TestValidateTableSQLiteDialect(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(SQLite()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatal("expected pass under sqlite dialect")
	}
}

func TestValidateTableDuckDBDialect(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(DuckDB()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatal("expected pass under duckdb dialect")
	}
}

func TestValidateTableStopsOnDatabaseError(t *testing.T) {
	db := openErrorDB(t)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err == nil {
		t.Fatal("expected database error")
	}
	if len(rep.Results) != 0 {
		t.Fatalf("partial results len = %d, want 0 on execution error", len(rep.Results))
	}
}

func TestValidateTableContinueOnErrorCollectsPartialResults(t *testing.T) {
	db := openErrorDB(t)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), CaptureQueryDiagnostics(), ContinueOnError(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError should not return execution error, got %v", err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(rep.Results))
	}
	if rep.Results[0].Err == nil {
		t.Fatal("first result should record execution error")
	}
	if rep.Results[0].diagnostics == nil || len(rep.Results[0].diagnostics.args) != 2 {
		t.Fatalf("execution-error diagnostics = %#v", rep.Results[0].diagnostics)
	}
	if rep.Results[1].Err == nil {
		t.Fatal("second result should record execution error")
	}
	if rep.Results[0].Name != "age between [0,120]" || rep.Results[1].Name != "email not empty" {
		t.Fatalf("declaration order = [%q, %q]", rep.Results[0].Name, rep.Results[1].Name)
	}
	if rep.OK() {
		t.Fatal("report should not be OK when expectations hit execution errors")
	}
}

func TestValidateTableRejectsInvalidKeyColumn(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openHarnessDB(t)

	_, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("bad-id"),
	)
	if err == nil {
		t.Fatal("expected configuration error for invalid key column")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v", err)
	}
}

func TestValidateTableRejectsNegativeFailedKeysCap(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openHarnessDB(t)

	_, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"), WithFailedKeysCap(-1),
	)
	if err == nil {
		t.Fatal("expected failed keys cap error")
	}
}

func TestValidateTableRejectsNegativeSampleCap(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openHarnessDB(t)

	_, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithSampleCap(-1),
	)
	if err == nil {
		t.Fatal("expected sample cap error")
	}
}

func TestInEmptyValuesReturnsConfigurationError(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active"},
	))
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(Column("status").In()).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected configuration error for empty IN list")
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before SQL on empty In configuration failure", counter.queries)
	}
}

func TestNotInEmptyValuesRejectedBeforeSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active"},
	))
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(Column("status").NotIn()).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected configuration error for empty NOT IN list")
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before SQL on empty NotIn configuration failure", counter.queries)
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v", err)
	}
}

func TestInNilValueReturnsConfigurationError(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active"},
	))
	db := openHarnessDB(t)

	_, err := NewSuite(Column("status").In("active", nil)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected configuration error for nil IN value")
	}
}

func TestNotInNilValueReturnsConfigurationError(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active"},
	))
	db := openHarnessDB(t)

	_, err := NewSuite(Column("status").NotIn("deleted", nil)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected configuration error for nil NOT IN value")
	}
}

func TestValidationErrorSupportsErrorsAs(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(200)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	verr := rep.Err()
	var ve *ValidationError
	if !errors.As(verr, &ve) {
		t.Fatalf("errors.As failed: %T", verr)
	}
	if len(ve.Report.Results) != 1 {
		t.Fatalf("wrapped report results = %d", len(ve.Report.Results))
	}
}

func TestValidateTableScopeThreadsPredicateAndIdentity(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "tenant-a",
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope(" tenant-run ", "tenant_id = ?", "tenant-a")),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	if rep.ScopeID != "tenant-run" {
		t.Fatalf("scope ID = %q, want tenant-run", rep.ScopeID)
	}
	if len(db.queries) == 0 {
		t.Fatal("valid scoped validation should execute SQL")
	}
	if !strings.Contains(db.queries[0].text, "tenant_id") {
		t.Fatalf("query = %q, want scope predicate", db.queries[0].text)
	}
}

func TestValidateTableInvalidScopeAbortsBeforeSQL(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
	}{
		{name: "blank identity", scope: TrustedScope(" ", "tenant_id = ?", "tenant-a")},
		{name: "missing predicate", scope: TrustedScope("tenant", "")},
		{name: "values without predicate", scope: TrustedScope("tenant", " ", "tenant-a")},
		{name: "placeholder arity mismatch", scope: TrustedScope("tenant", "tenant_id = ?", "a", "b")},
		{name: "unsupported question mark", scope: TrustedScope("tenant", "note = 'what?'")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setHarnessData(t, scopedHarnessUsers("tenant_id", "tenant-a",
				map[string]any{"id": int64(1), "age": int64(25)},
			))
			db := openCountingHarnessDB(t)

			rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
				context.Background(), db, Table("users"),
				WithDialect(Postgres()), WithScope(tc.scope),
			)
			if err == nil {
				t.Fatal("expected invalid scope error")
			}
			if len(rep.Results) != 0 || rep.Target != nil || rep.ScopeID != "" {
				t.Fatalf("report = %#v, want zero report", rep)
			}
			if db.queries != 0 {
				t.Fatalf("queries = %d, want 0", db.queries)
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) && tc.name != "unsupported question mark" {
				t.Fatalf("error category = %v, want invalid_config", err)
			}
		})
	}
}

func TestValidateTableInvalidScopeAbortsWithContinueOnError(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "tenant-a",
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openCountingHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ? AND region = ?", "tenant-a")),
		ContinueOnError(),
	)
	if err == nil {
		t.Fatal("expected invalid scope error")
	}
	if len(rep.Results) != 0 || rep.Target != nil || rep.ScopeID != "" {
		t.Fatalf("report = %#v, want zero report", rep)
	}
	if db.queries != 0 {
		t.Fatalf("queries = %d, want 0", db.queries)
	}
}

func TestValidateTableScopeCopiesCallerBytes(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "tenant-a",
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openRecordingHarnessDB(t)
	payload := []byte("tenant-a")
	scope := TrustedScope("tenant", "tenant_id = ?", payload)
	payload[0] = 'x'

	_, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithScope(scope),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	if len(db.queries) == 0 || len(db.queries[0].args) == 0 {
		t.Fatalf("queries = %#v, want scoped argument", db.queries)
	}
	got, ok := db.queries[0].args[0].([]byte)
	if !ok || string(got) != "tenant-a" {
		t.Fatalf("scope argument = %#v, want copied tenant-a bytes", db.queries[0].args[0])
	}
}

func assertSemanticReportParity(t *testing.T, got, want Report) {
	t.Helper()
	if got.ScopeID != want.ScopeID {
		t.Fatalf("ScopeID = %q, want %q", got.ScopeID, want.ScopeID)
	}
	if (got.Target == nil) != (want.Target == nil) {
		t.Fatalf("Target nil mismatch: got %#v want %#v", got.Target, want.Target)
	}
	if got.Target != nil && want.Target != nil && *got.Target != *want.Target {
		t.Fatalf("Target = %#v, want %#v", *got.Target, *want.Target)
	}
	if len(got.Results) != len(want.Results) {
		t.Fatalf("results len = %d, want %d", len(got.Results), len(want.Results))
	}
	for i := range got.Results {
		g := stripResultDiagnostics(got.Results[i])
		w := stripResultDiagnostics(want.Results[i])
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("result[%d] semantic mismatch\ngot:  %#v\nwant: %#v", i, g, w)
		}
	}
}

func TestSharedScalarEvaluationDefaultRemainsSequential(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(200), "email": ""},
	))
	db := openRecordingHarnessDB(t)

	_, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	if got := countFailureCountQueries(db.queries); got != 2 {
		t.Fatalf("failure count queries = %d, want 2 without WithSharedScalarEvaluation", got)
	}
	if got := len(scopedDenominatorTotals(db.queries)); got != 1 {
		t.Fatalf("scoped denominator totals = %d, want 1", got)
	}
}

func TestSharedScalarEvaluationMixedDeclarationOrderParity(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(200), "email": ""},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(10), "email": ""},
	))
	suite := NewSuite(
		WithID("age-ok", Int("age").Between(0, 120)),
		WithID("rows", RowCount().Equal(2)),
		WithID("email-ok", String("email").NotEmpty()),
	)
	opts := []Option{
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
		WithKey("id"),
		WithSampleCap(5),
	}

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

	wantKinds := []ExpectationKind{KindBetween, KindRowCountEqual, KindNotEmpty}
	wantIDs := []string{"age-ok", "rows", "email-ok"}
	if len(shared.Results) != len(wantKinds) {
		t.Fatalf("results len = %d, want %d", len(shared.Results), len(wantKinds))
	}
	for i, kind := range wantKinds {
		if shared.Results[i].Kind != kind {
			t.Fatalf("result[%d] Kind = %q, want %q", i, shared.Results[i].Kind, kind)
		}
		if shared.Results[i].ID != wantIDs[i] {
			t.Fatalf("result[%d] ID = %q, want %q", i, shared.Results[i].ID, wantIDs[i])
		}
	}
	assertSemanticReportParity(t, shared, seq)
}

func TestSharedScalarEvaluationReportParityExcludingQueryDiagnostics(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(200), "email": ""},
		map[string]any{"id": int64(3), "age": int64(150), "email": "ok@x.com"},
	))
	suite := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
		Column("id").NotNull(),
	)
	opts := []Option{
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
		WithKey("id"),
		WithSampleCap(5),
		CaptureQueryDiagnostics(),
	}

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
}

func TestSharedScalarEvaluationScopeAndToleranceParity(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(200), "email": "b@b.com"},
		map[string]any{"id": int64(3), "age": int64(30), "email": ""},
		map[string]any{"id": int64(4), "tenant_id": "t2", "age": int64(999), "email": ""},
	))
	suite := NewSuite(
		WithMaxFailedCount(1, Int("age").Between(0, 120)),
		WithMaxFailedCount(1, String("email").NotEmpty()),
	)
	opts := []Option{
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant-run", "tenant_id = ?", "t1")),
		WithKey("id"),
		WithSampleCap(5),
	}

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
	if shared.ScopeID != "tenant-run" {
		t.Fatalf("ScopeID = %q, want tenant-run", shared.ScopeID)
	}
	for i, res := range shared.Results {
		if res.FailedCount != 1 || !res.Success || !res.Tolerated {
			t.Fatalf("result[%d] FailedCount=%d Success=%v Tolerated=%v, want 1/true/true",
				i, res.FailedCount, res.Success, res.Tolerated)
		}
		if res.Facts.ConfiguredMaxFailedCount == nil || *res.Facts.ConfiguredMaxFailedCount != 1 {
			t.Fatalf("result[%d] ConfiguredMaxFailedCount = %v, want 1", i, res.Facts.ConfiguredMaxFailedCount)
		}
	}
	assertSemanticReportParity(t, shared, seq)
}

func TestSharedScalarEvaluationDatabaseErrorWithoutContinueOnErrorZeroReport(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
	))
	scope := TrustedScope("tenant", "tenant_id = ?", "t1")
	injected := fmt.Errorf("injected shared scalar database failure")
	db := openSharedScalarErrorHarnessDB(t, injected, false)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
		RowCount().Equal(2),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
		SummaryOnly(),
		WithSharedScalarEvaluation(),
	)
	if err == nil {
		t.Fatal("expected shared scalar database error")
	}
	if !errors.Is(err, ErrCategoryDatabase) {
		t.Fatalf("error category = %v, want database", err)
	}
	if len(rep.Results) != 0 || rep.Target != nil || rep.ScopeID != "" {
		t.Fatalf("report = %#v, want zero report", rep)
	}
	assertSharedScalarNoSequentialFallback(t, db.queries)
}

func TestSharedScalarEvaluationScanErrorWithoutContinueOnErrorZeroReport(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
	))
	scope := TrustedScope("tenant", "tenant_id = ?", "t1")
	db := openSharedScalarErrorHarnessDB(t, nil, true)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
		SummaryOnly(),
		WithSharedScalarEvaluation(),
	)
	if err == nil {
		t.Fatal("expected shared scalar scan error")
	}
	if !errors.Is(err, ErrCategoryScan) {
		t.Fatalf("error category = %v, want scan", err)
	}
	if len(rep.Results) != 0 || rep.Target != nil || rep.ScopeID != "" {
		t.Fatalf("report = %#v, want zero report", rep)
	}
	assertSharedScalarNoSequentialFallback(t, db.queries)
}

func TestSharedScalarEvaluationCanceledContextWithoutContinueOnErrorZeroReport(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
	))
	scope := TrustedScope("tenant", "tenant_id = ?", "t1")
	db := openSharedScalarErrorHarnessDB(t, context.Canceled, false)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
		RowCount().Equal(2),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
		SummaryOnly(),
		WithSharedScalarEvaluation(),
	)
	if err == nil {
		t.Fatal("expected shared scalar context cancellation error")
	}
	if !errors.Is(err, ErrCategoryContext) && !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context category", err)
	}
	if len(rep.Results) != 0 || rep.Target != nil || rep.ScopeID != "" {
		t.Fatalf("report = %#v, want zero report", rep)
	}
	assertSharedScalarNoSequentialFallback(t, db.queries)
}

func TestSharedScalarEvaluationSharedErrorContinueOnErrorAffectsOnlyCompatibleSlots(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com", "amount": float64(10)},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com", "amount": float64(20)},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(10), "email": "c@c.com", "amount": float64(100)},
	))
	scope := TrustedScope("tenant", "tenant_id = ?", "t1")
	injected := fmt.Errorf("injected shared scalar database failure")
	db := openSharedScalarErrorHarnessDB(t, injected, false)

	rep, err := NewSuite(
		WithID("age-check", Int("age").Between(0, 120)),
		WithID("email-check", String("email").NotEmpty()),
		WithID("rows", RowCount().Equal(2)),
		WithID("avg-amount", Float("amount").AverageBetween(10, 20)),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
		SummaryOnly(),
		ContinueOnError(),
		WithSharedScalarEvaluation(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
	}
	if len(rep.Results) != 4 {
		t.Fatalf("results len = %d, want 4", len(rep.Results))
	}

	age, email := rep.Results[0], rep.Results[1]
	if age.Err == nil || email.Err == nil {
		t.Fatalf("compatible slots must record shared error: %#v, %#v", age, email)
	}
	if !errors.Is(age.Err, ErrCategoryDatabase) || !errors.Is(email.Err, ErrCategoryDatabase) {
		t.Fatalf("compatible errors = %v and %v, want database category", age.Err, email.Err)
	}
	if age.Err.Error() != email.Err.Error() {
		t.Fatalf("compatible errors differ:\n[%s] %v\n[%s] %v", age.ID, age.Err, email.ID, email.Err)
	}
	if age.Success || email.Success {
		t.Fatal("compatible slots must not succeed when shared statement fails")
	}
	if age.ID != "age-check" || email.ID != "email-check" {
		t.Fatalf("compatible IDs = %q, %q", age.ID, email.ID)
	}

	rows := rep.Results[2]
	if rows.Err != nil {
		t.Fatalf("incompatible RowCount should execute independently: %v", rows.Err)
	}
	if !rows.Success || rows.ID != "rows" {
		t.Fatalf("RowCount result = %#v, want successful rows slot", rows)
	}
	if rows.Facts.ObservedCount == nil || *rows.Facts.ObservedCount != 2 {
		t.Fatalf("RowCount observed = %v, want 2", rows.Facts.ObservedCount)
	}

	aggregate := rep.Results[3]
	if aggregate.Err != nil {
		t.Fatalf("incompatible aggregate should execute independently: %v", aggregate.Err)
	}
	if !aggregate.Success || aggregate.ID != "avg-amount" {
		t.Fatalf("aggregate result = %#v, want successful avg-amount slot", aggregate)
	}
	if aggregate.Facts.ObservedFloat == nil || *aggregate.Facts.ObservedFloat != 15 {
		t.Fatalf("aggregate observed = %v, want 15", aggregate.Facts.ObservedFloat)
	}

	assertSharedScalarNoSequentialFallback(t, db.queries)
}

func TestSharedScalarEvaluationCanceledContextContinueOnErrorAffectsOnlyCompatibleSlots(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(10), "email": "c@c.com"},
	))
	scope := TrustedScope("tenant", "tenant_id = ?", "t1")
	db := openSharedScalarErrorHarnessDB(t, context.Canceled, false)

	rep, err := NewSuite(
		WithID("age-check", Int("age").Between(0, 120)),
		WithID("email-check", String("email").NotEmpty()),
		WithID("rows", RowCount().Equal(2)),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
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

	for i, res := range rep.Results[:2] {
		if res.Err == nil || (!errors.Is(res.Err, ErrCategoryContext) && !errors.Is(res.Err, context.Canceled)) {
			t.Fatalf("compatible result[%d] = %#v, want context cancellation", i, res)
		}
		if res.Success {
			t.Fatalf("compatible result[%d] must not succeed", i)
		}
	}

	rows := rep.Results[2]
	if rows.Err != nil {
		t.Fatalf("later incompatible RowCount should still execute: %v", rows.Err)
	}
	if !rows.Success {
		t.Fatalf("RowCount result = %#v, want success", rows)
	}
	if rows.Facts.ObservedCount == nil || *rows.Facts.ObservedCount != 2 {
		t.Fatalf("RowCount observed = %v, want 2", rows.Facts.ObservedCount)
	}

	assertSharedScalarNoSequentialFallback(t, db.queries)
}

func TestSharedScalarEvaluationScanErrorContinueOnErrorAffectsOnlyCompatibleSlots(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
	))
	scope := TrustedScope("tenant", "tenant_id = ?", "t1")
	db := openSharedScalarErrorHarnessDB(t, nil, true)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("email").NotEmpty(),
		RowCount().Equal(2),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
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
	for i, res := range rep.Results[:2] {
		if res.Err == nil || !errors.Is(res.Err, ErrCategoryScan) {
			t.Fatalf("compatible result[%d] = %#v, want scan category", i, res)
		}
		if res.Success {
			t.Fatalf("compatible result[%d] must not succeed", i)
		}
	}
	if rep.Results[2].Err != nil || !rep.Results[2].Success {
		t.Fatalf("incompatible RowCount = %#v, want independent success", rep.Results[2])
	}
	assertSharedScalarNoSequentialFallback(t, db.queries)
}
