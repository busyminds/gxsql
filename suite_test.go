package gxsql

import (
	"context"
	"errors"
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
