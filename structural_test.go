package gxsql

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func setStructuralUsers(t *testing.T, cols []string, rows ...map[string]any) {
	t.Helper()
	setHarnessColumns(t, map[string][]string{"users": append([]string(nil), cols...)})
	setHarnessData(t, harnessUsers(rows...))
}

func assertStructuralPass(t *testing.T, res Result, kind ExpectationKind, required []string) {
	t.Helper()
	if res.Err != nil {
		t.Fatalf("Err = %v", res.Err)
	}
	if !res.Success || res.Kind != kind {
		t.Fatalf("result = %#v, want success kind=%q", res, kind)
	}
	if res.Column != "" {
		t.Fatalf("Column = %q, want blank", res.Column)
	}
	if res.RowDenominator != RowDenominatorUnavailable || res.Total != 0 || res.FailedCount != 0 || res.FailedPercent != 0 {
		t.Fatalf("table-level shape = %#v", res)
	}
	if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("diagnostics = samples %#v keys %#v, want empty", res.SampleValues, res.FailedKeys)
	}
	if !reflect.DeepEqual(res.Facts.RequiredColumns, required) {
		t.Fatalf("RequiredColumns = %#v, want %#v", res.Facts.RequiredColumns, required)
	}
	if len(res.Facts.MissingColumns) != 0 || len(res.Facts.UnexpectedColumns) != 0 {
		t.Fatalf("difference facts = missing %#v unexpected %#v", res.Facts.MissingColumns, res.Facts.UnexpectedColumns)
	}
}

func assertStructuralFail(t *testing.T, res Result, kind ExpectationKind, required, missing, unexpected []string) {
	t.Helper()
	if res.Err != nil {
		t.Fatalf("Err = %v", res.Err)
	}
	if res.Success || res.Kind != kind {
		t.Fatalf("result = %#v, want failure kind=%q", res, kind)
	}
	if res.Column != "" || res.RowDenominator != RowDenominatorUnavailable {
		t.Fatalf("shape = %#v", res)
	}
	if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("diagnostics = samples %#v keys %#v, want empty", res.SampleValues, res.FailedKeys)
	}
	if !reflect.DeepEqual(res.Facts.RequiredColumns, required) {
		t.Fatalf("RequiredColumns = %#v, want %#v", res.Facts.RequiredColumns, required)
	}
	if !reflect.DeepEqual(res.Facts.MissingColumns, missing) {
		t.Fatalf("MissingColumns = %#v, want %#v", res.Facts.MissingColumns, missing)
	}
	if !reflect.DeepEqual(res.Facts.UnexpectedColumns, unexpected) {
		t.Fatalf("UnexpectedColumns = %#v, want %#v", res.Facts.UnexpectedColumns, unexpected)
	}
}

func TestRequiredColumnsPassAllowsExtra(t *testing.T) {
	setStructuralUsers(t, []string{"id", "event_time", "payload", "note"})
	db := openRecordingHarnessDB(t)

	required := []string{"id", "event_time", "payload"}
	rep, err := NewSuite(RequiredColumns(required...)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralPass(t, rep.Results[0], KindRequiredColumns, required)
	assertZeroRowDiscoverySQL(t, db, `"users"`)
}

func TestExactColumnsPassExactSet(t *testing.T) {
	cols := []string{"id", "event_time", "payload"}
	setStructuralUsers(t, cols)
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(ExactColumns(cols...)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralPass(t, rep.Results[0], KindExactColumns, cols)
	assertZeroRowDiscoverySQL(t, db, `"users"`)
}

func TestRequiredColumnsMissingNames(t *testing.T) {
	setStructuralUsers(t, []string{"id", "payload"})
	db := openHarnessDB(t)

	required := []string{"id", "event_time", "payload"}
	rep, err := NewSuite(RequiredColumns(required...)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralFail(t, rep.Results[0], KindRequiredColumns, required, []string{"event_time"}, nil)
}

func TestExactColumnsUnexpectedNames(t *testing.T) {
	setStructuralUsers(t, []string{"id", "event_time", "payload", "note"})
	db := openHarnessDB(t)

	required := []string{"id", "event_time", "payload"}
	rep, err := NewSuite(ExactColumns(required...)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralFail(t, rep.Results[0], KindExactColumns, required, nil, []string{"note"})
}

func TestExactColumnsMissingAndUnexpected(t *testing.T) {
	setStructuralUsers(t, []string{"id", "payload", "note"})
	db := openHarnessDB(t)

	required := []string{"id", "event_time", "payload"}
	rep, err := NewSuite(ExactColumns(required...)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralFail(t, rep.Results[0], KindExactColumns, required, []string{"event_time"}, []string{"note"})
	if !strings.Contains(rep.Results[0].Name, "missing event_time") || !strings.Contains(rep.Results[0].Name, "unexpected note") {
		t.Fatalf("Name = %q", rep.Results[0].Name)
	}
}

func TestStructuralReorderedDiscoveryIndependent(t *testing.T) {
	required := []string{"id", "event_time", "payload"}
	for _, discovered := range [][]string{
		{"id", "event_time", "payload"},
		{"payload", "id", "event_time"},
		{"event_time", "payload", "id"},
	} {
		t.Run(strings.Join(discovered, ","), func(t *testing.T) {
			setStructuralUsers(t, discovered)
			db := openHarnessDB(t)
			rep, err := NewSuite(
				RequiredColumns(required...),
				ExactColumns(required...),
			).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
			if err != nil {
				t.Fatal(err)
			}
			assertStructuralPass(t, rep.Results[0], KindRequiredColumns, required)
			assertStructuralPass(t, rep.Results[1], KindExactColumns, required)
		})
	}
}

func TestExactColumnsUnexpectedFollowsDiscoveryOrder(t *testing.T) {
	setStructuralUsers(t, []string{"note", "id", "extra", "payload"})
	db := openHarnessDB(t)

	required := []string{"id", "payload"}
	rep, err := NewSuite(ExactColumns(required...)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralFail(t, rep.Results[0], KindExactColumns, required, nil, []string{"note", "extra"})
}

func TestRequiredColumnsMissingFollowsDeclarationOrder(t *testing.T) {
	setStructuralUsers(t, []string{"payload"})
	db := openHarnessDB(t)

	required := []string{"event_time", "id", "payload"}
	rep, err := NewSuite(RequiredColumns(required...)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralFail(t, rep.Results[0], KindRequiredColumns, required, []string{"event_time", "id"}, nil)
}

func TestStructuralPhysicalCaseNoFolding(t *testing.T) {
	setStructuralUsers(t, []string{"id", "EventTime", "payload"})
	db := openHarnessDB(t)

	wrong := []string{"id", "eventtime", "payload"}
	rep, err := NewSuite(RequiredColumns(wrong...)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralFail(t, rep.Results[0], KindRequiredColumns, wrong, []string{"eventtime"}, nil)

	right := []string{"id", "EventTime", "payload"}
	rep, err = NewSuite(ExactColumns(right...)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralPass(t, rep.Results[0], KindExactColumns, right)
}

func TestStructuralInvalidAndDuplicateExpectedNamesZeroSQL(t *testing.T) {
	setStructuralUsers(t, []string{"id", "payload"})
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(
		RequiredColumns("id", "bad-name"),
		ExactColumns("id", "id"),
		RequiredColumns(),
	).ValidateTable(context.Background(), counter, Table("users"), WithDialect(Postgres()))
	if err == nil {
		t.Fatal("expected preflight errors")
	}
	var pf *PreflightErrors
	if !errors.As(err, &pf) {
		t.Fatalf("err = %T (%v), want *PreflightErrors", err, err)
	}
	if len(pf.Issues) != 3 {
		t.Fatalf("issues = %#v, want 3", pf.Issues)
	}
	for _, iss := range pf.Issues {
		if !errors.Is(iss.Err, ErrCategoryInvalidConfig) {
			t.Fatalf("issue %#v category", iss)
		}
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0", counter.queries)
	}
}

func TestStructuralScopePreflightDefaultAndContinueOnError(t *testing.T) {
	setStructuralUsers(t, []string{"id", "event_time", "payload"})
	scope := TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")

	counter := openCountingHarnessDB(t)
	_, err := NewSuite(RequiredColumns("id")).ValidateTable(
		context.Background(), counter, Table("users"),
		WithDialect(Postgres()), WithScope(scope),
	)
	if err == nil {
		t.Fatal("expected scope preflight error")
	}
	var pf *PreflightErrors
	if !errors.As(err, &pf) {
		t.Fatalf("err = %T (%v), want *PreflightErrors", err, err)
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v", err)
	}
	if !strings.Contains(err.Error(), "population filters are incompatible with structural column expectations") {
		t.Fatalf("message = %v", err)
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0", counter.queries)
	}

	counter = openCountingHarnessDB(t)
	rep, err := NewSuite(
		RequiredColumns("id"),
		RowCount().Equal(0),
		ExactColumns("id", "event_time", "payload"),
	).ValidateTable(
		context.Background(), counter, Table("users"),
		WithDialect(Postgres()), WithScope(scope), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results = %d", len(rep.Results))
	}
	if rep.Results[0].Err == nil || !errors.Is(rep.Results[0].Err, ErrCategoryInvalidConfig) {
		t.Fatalf("required scope slot = %#v", rep.Results[0])
	}
	if !rep.Results[1].Success || rep.Results[1].Err != nil {
		t.Fatalf("row count should still run: %#v", rep.Results[1])
	}
	if rep.Results[2].Err == nil || !errors.Is(rep.Results[2].Err, ErrCategoryInvalidConfig) {
		t.Fatalf("exact scope slot = %#v", rep.Results[2])
	}
	if counter.queries == 0 {
		t.Fatal("valid expectations should execute under ContinueOnError")
	}
}

func TestStructuralMissingTargetTypedExecutionError(t *testing.T) {
	setStructuralUsers(t, []string{"id"})
	db := openHarnessDB(t)

	_, err := NewSuite(RequiredColumns("id")).ValidateTable(
		context.Background(), db, Table("missing_table"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected missing target execution error")
	}
	if !errors.Is(err, ErrCategoryDatabase) {
		t.Fatalf("category = %v, want database", err)
	}

	rep, err := NewSuite(RequiredColumns("id"), ExactColumns("id")).ValidateTable(
		context.Background(), db, Table("missing_table"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results = %d", len(rep.Results))
	}
	for i, res := range rep.Results {
		if res.Success || res.Err == nil || !errors.Is(res.Err, ErrCategoryDatabase) {
			t.Fatalf("result %d = %#v", i, res)
		}
		if !reflect.DeepEqual(res.Facts.RequiredColumns, []string{"id"}) {
			t.Fatalf("result %d RequiredColumns = %#v", i, res.Facts.RequiredColumns)
		}
		if len(res.Facts.MissingColumns) != 0 || len(res.Facts.UnexpectedColumns) != 0 {
			t.Fatalf("result %d should not publish differences on execution error: %#v", i, res.Facts)
		}
	}
}

func TestStructuralCancellation(t *testing.T) {
	setStructuralUsers(t, []string{"id", "payload"})
	db := openHarnessDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewSuite(ExactColumns("id", "payload")).ValidateTable(
		ctx, db, Table("users"), WithDialect(Postgres()),
	)
	if !errors.Is(err, ErrCategoryContext) && !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context category", err)
	}
}

func TestStructuralNoSamplesOrFailedKeys(t *testing.T) {
	setStructuralUsers(t, []string{"id", "note"})
	db := openHarnessDB(t)

	rep, err := NewSuite(ExactColumns("id", "payload")).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"), WithSampleCap(10), WithFailedKeysCap(10),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	assertStructuralFail(t, res, KindExactColumns, []string{"id", "payload"}, []string{"payload"}, []string{"note"})
}

func TestStructuralReadOnlyZeroRowSQLShape(t *testing.T) {
	setStructuralUsers(t, []string{"id", "event_time", "payload"},
		map[string]any{"id": int64(1), "event_time": "2026-01-01", "payload": "x"},
	)
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(ExactColumns("id", "event_time", "payload")).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), CaptureQueryDiagnostics(),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralPass(t, rep.Results[0], KindExactColumns, []string{"id", "event_time", "payload"})
	assertZeroRowDiscoverySQL(t, db, `"users"`)
	if rep.Results[0].diagnostics == nil || rep.Results[0].diagnostics.query == "" {
		t.Fatal("expected captured discovery SQL")
	}
	if len(rep.Results[0].diagnostics.args) != 0 {
		t.Fatalf("discovery args = %#v, want none", rep.Results[0].diagnostics.args)
	}
}

func TestStructuralDialectQuotingVariants(t *testing.T) {
	cols := []string{"id", "payload"}
	setStructuralUsers(t, cols)

	for _, tc := range []struct {
		name    string
		dialect Dialect
		table   string
	}{
		{name: "postgres", dialect: Postgres(), table: `"users"`},
		{name: "sqlite", dialect: SQLite(), table: `"users"`},
		{name: "duckdb", dialect: DuckDB(), table: `"users"`},
		{name: "mysql", dialect: MySQL(), table: "`users`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openRecordingHarnessDB(t)
			rep, err := NewSuite(ExactColumns(cols...)).ValidateTable(
				context.Background(), db, Table("users"), WithDialect(tc.dialect),
			)
			if err != nil {
				t.Fatal(err)
			}
			assertStructuralPass(t, rep.Results[0], KindExactColumns, cols)
			assertZeroRowDiscoverySQL(t, db, tc.table)
		})
	}
}

func assertZeroRowDiscoverySQL(t *testing.T, db *recordingDB, quotedTable string) {
	t.Helper()
	if len(db.queries) != 1 {
		t.Fatalf("queries = %#v, want exactly one discovery probe", db.queries)
	}
	q := db.queries[0]
	want := "SELECT * FROM " + quotedTable + " WHERE 1 = 0"
	if q.text != want {
		t.Fatalf("query = %q, want %q", q.text, want)
	}
	if len(q.args) != 0 {
		t.Fatalf("args = %#v, want none", q.args)
	}
	lower := strings.ToLower(q.text)
	for _, forbidden := range []string{
		"insert ", "update ", "delete ", "alter ", "drop ", "create ", "truncate ",
		"information_schema", "pg_catalog", "sqlite_master",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("discovery SQL %q contains forbidden %q", q.text, forbidden)
		}
	}
}
