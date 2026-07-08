package gxsql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestInvalidIdentifierCollectedBeforeSQL(t *testing.T) {
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(Int("bad-column").Between(0, 120)).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected invalid identifier configuration error")
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before SQL on config failure", counter.queries)
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v", err)
	}
}

func TestInvalidIdentifierConfigSlotAfterValidExpectationUnderContinueOnError(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	counter := openCountingHarnessDB(t)

	rep, err := NewSuite(
		WithID("good", Int("age").Between(0, 120)),
		WithID("bad", Int("bad-column").Between(0, 120)),
	).ValidateTable(
		context.Background(), counter, Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results len = %d", len(rep.Results))
	}
	if !rep.Results[0].Success {
		t.Fatal("valid expectation should pass")
	}
	if rep.Results[1].Success || rep.Results[1].Err == nil {
		t.Fatal("invalid identifier should occupy config failure slot")
	}
	if !errors.Is(rep.Results[1].Err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v", rep.Results[1].Err)
	}
	if counter.queries == 0 {
		t.Fatal("valid expectation should execute SQL under ContinueOnError")
	}
}

func TestLenEqualAssignsStableKind(t *testing.T) {
	exp := String("country_code").LenEqual(2)
	if kind := expectationKind(exp); kind != KindLenEqual {
		t.Fatalf("Kind = %q, want %q", kind, KindLenEqual)
	}
}

func TestEveryBuiltinExpectationHasNonemptyStableKind(t *testing.T) {
	builtins := []Expectation{
		Column("c").IsNull(),
		Column("c").NotNull(),
		Column("c").In("a"),
		Column("c").NotIn("a"),
		Column("c").Unique(),
		Int("c").Between(0, 1),
		Int("c").GreaterThan(0),
		Int("c").LessThan(10),
		Int("c").GreaterOrEqual(0),
		Int("c").LessOrEqual(10),
		String("c").NotEmpty(),
		String("c").Empty(),
		String("c").LenEqual(2),
		String("c").LenBetween(1, 3),
		RowCount().Equal(1),
		RowCount().Between(0, 10),
		RowCount().GreaterThan(0),
		RowCount().GreaterOrEqual(1),
		RowCount().LessThan(10),
		RowCount().LessOrEqual(5),
		Column("c").DistinctCount().Equal(1),
		Column("c").DistinctCount().Between(1, 3),
		Column("c").DistinctCount().GreaterThan(0),
		Column("c").DistinctCount().GreaterOrEqual(1),
		Column("c").DistinctCount().LessThan(10),
		Column("c").DistinctCount().LessOrEqual(5),
		Float("c").AverageBetween(0, 1),
		Float("c").MinGreaterOrEqual(0),
		Float("c").MaxLessOrEqual(100),
	}
	for i, exp := range builtins {
		kind := expectationKind(exp)
		if kind == "" || kind == KindCustom {
			t.Fatalf("builtin[%d] %T has kind %q", i, exp, kind)
		}
	}
}

func TestWithIDNilInnerSafeUnderContinueOnError(t *testing.T) {
	rep, err := NewSuite(WithID("nil-wrap", nil)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 1 {
		t.Fatalf("results len = %d", len(rep.Results))
	}
	if rep.Results[0].Success || rep.Results[0].Err == nil {
		t.Fatal("nil inner expectation should fail safely")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v", rep.Results[0].Err)
	}
}

func TestConfiguredThresholdsStructuredInResultFacts(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "amount": float64(42), "country_code": "US"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		String("country_code").LenEqual(2),
		RowCount().Equal(1),
		Float("amount").AverageBetween(0, 100),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}

	between := rep.Results[0].Facts
	if between.ConfiguredBoundLower == nil || between.ConfiguredBoundUpper == nil {
		t.Fatalf("between facts = %#v", between)
	}

	lenEq := rep.Results[1].Facts
	if lenEq.ConfiguredCount == nil || *lenEq.ConfiguredCount != 2 {
		t.Fatalf("len equal facts = %#v", lenEq)
	}

	rowCount := rep.Results[2].Facts
	if rowCount.ConfiguredCount == nil || *rowCount.ConfiguredCount != 1 {
		t.Fatalf("row count facts = %#v", rowCount)
	}

	avg := rep.Results[3].Facts
	if avg.ConfiguredFloatLower == nil || avg.ConfiguredFloatUpper == nil || avg.ObservedFloat == nil {
		t.Fatalf("aggregate facts = %#v", avg)
	}
}

func TestCaptureDiagnosticsUniqueAndAggregate(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "a@b.com"},
		map[string]any{"id": int64(2), "email": "a@b.com"},
		map[string]any{"id": int64(3), "amount": float64(10)},
		map[string]any{"id": int64(4), "amount": float64(200)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		Column("email").Unique(),
		Float("amount").AverageBetween(0, 50),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), CaptureQueryDiagnostics(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].diagnostics == nil || !strings.Contains(rep.Results[0].diagnostics.query, "COUNT") {
		t.Fatalf("unique diagnostics = %#v", rep.Results[0].diagnostics)
	}
	if rep.Results[1].diagnostics == nil || !strings.Contains(rep.Results[1].diagnostics.query, "AVG") {
		t.Fatalf("aggregate diagnostics = %#v", rep.Results[1].diagnostics)
	}
}

func TestScalarCountQueryFailureCategorizedAsDatabase(t *testing.T) {
	db := openErrorDB(t)
	_, err := queryScalarInt(context.Background(), db, "SELECT COUNT(*) FROM users")
	if err == nil {
		t.Fatal("expected database error")
	}
	if !errors.Is(err, ErrCategoryDatabase) {
		t.Fatalf("category = %v", err)
	}
	var ce *CategorizedError
	if !errors.As(err, &ce) || ce.Category != CategoryDatabase {
		t.Fatalf("got %#v", err)
	}
}

func TestScalarCountScanFailureCategorizedAsScan(t *testing.T) {
	db := openScanErrorDB(t)
	_, err := queryScalarInt(context.Background(), db, "SELECT COUNT(*) FROM users")
	if err == nil {
		t.Fatal("expected scan error")
	}
	if !errors.Is(err, ErrCategoryScan) {
		t.Fatalf("category = %v", err)
	}
	var ce *CategorizedError
	if !errors.As(err, &ce) || ce.Category != CategoryScan {
		t.Fatalf("got %#v", err)
	}
}

func TestScalarCountCanceledContextCategorizedAsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := queryScalarInt(ctx, openErrorDB(t), "SELECT COUNT(*) FROM users")
	if err == nil {
		t.Fatal("expected context error")
	}
	if !errors.Is(err, ErrCategoryContext) {
		t.Fatalf("category = %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("expected errors.Is context.Canceled through categorized error")
	}
}

func TestPerRowQueryFailureCategorizedAsDatabase(t *testing.T) {
	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), openErrorDB(t), Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Err == nil {
		t.Fatal("expected execution error on result")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryDatabase) {
		t.Fatalf("category = %v", rep.Results[0].Err)
	}
}

func TestAggregateQueryFailureCategorizedAsDatabase(t *testing.T) {
	rep, err := NewSuite(Float("amount").AverageBetween(0, 100)).ValidateTable(
		context.Background(), openErrorDB(t), Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Err == nil {
		t.Fatal("expected execution error on result")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryDatabase) {
		t.Fatalf("category = %v", rep.Results[0].Err)
	}
}

func TestAggregateScanFailureCategorizedAsScan(t *testing.T) {
	rep, err := NewSuite(Float("amount").AverageBetween(0, 100)).ValidateTable(
		context.Background(), openScanErrorDB(t), Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Err == nil {
		t.Fatal("expected scan error on result")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryScan) {
		t.Fatalf("category = %v", rep.Results[0].Err)
	}
}

func TestCapturePerRowPredicateArgumentsInDiagnostics(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active"},
		map[string]any{"id": int64(2), "status": "inactive"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("status").In("active", "pending")).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), CaptureQueryDiagnostics(),
	)
	if err != nil {
		t.Fatal(err)
	}
	d := rep.Results[0].diagnostics
	if d == nil {
		t.Fatal("expected captured diagnostics")
	}
	if len(d.args) < 2 {
		t.Fatalf("args = %#v, want predicate placeholders", d.args)
	}
}

func TestCaptureDiagnosticsOnExecutionErrorContinueOnError(t *testing.T) {
	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), openErrorDB(t), Table("users"),
		WithDialect(Postgres()), ContinueOnError(), CaptureQueryDiagnostics(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Err == nil {
		t.Fatal("expected execution error on result")
	}
	if rep.Results[0].diagnostics == nil || rep.Results[0].diagnostics.query == "" {
		t.Fatalf("diagnostics = %#v, want captured query on execution error", rep.Results[0].diagnostics)
	}
}

func TestAggregateTableRenderFailureCategorizedAsRendering(t *testing.T) {
	rep, err := NewSuite(Float("amount").AverageBetween(0, 100)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("bad table"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Err == nil {
		t.Fatal("expected rendering error on result")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryRendering) {
		t.Fatalf("category = %v", rep.Results[0].Err)
	}
}

func TestDistinctCountTableRenderFailureCategorizedAsRendering(t *testing.T) {
	rep, err := NewSuite(Column("email").DistinctCount().Equal(1)).ValidateTable(
		context.Background(), openHarnessDB(t), Table("bad table"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Err == nil {
		t.Fatal("expected rendering error on result")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryRendering) {
		t.Fatalf("category = %v", rep.Results[0].Err)
	}
}

func TestScalarCountCloseFailureCategorizedAsScan(t *testing.T) {
	_, err := queryScalarInt(context.Background(), openCloseErrorDB(t), "SELECT COUNT(*) FROM users")
	if err == nil {
		t.Fatal("expected close error")
	}
	if !errors.Is(err, ErrCategoryScan) {
		t.Fatalf("category = %v", err)
	}
	var ce *CategorizedError
	if !errors.As(err, &ce) || ce.Category != CategoryScan {
		t.Fatalf("got %#v", err)
	}
}

func TestAggregateCloseFailureCategorizedAsScan(t *testing.T) {
	_, _, _, err := queryAggregateFloat(
		context.Background(), openCloseErrorDB(t), Table("users"), evalOptions{dialect: Postgres()}, "amount", "AVG",
	)
	if err == nil {
		t.Fatal("expected close error")
	}
	if !errors.Is(err, ErrCategoryScan) {
		t.Fatalf("category = %v", err)
	}
}

type selectiveRenderDialect struct {
	Dialect
	fail map[string]bool
}

func (d selectiveRenderDialect) QuoteIdent(name string) (string, error) {
	if d.fail[name] {
		return "", fmt.Errorf("gxsqltest: render %q", name)
	}
	return d.Dialect.QuoteIdent(name)
}

func TestFailedKeysRenderFailureCategorizedAsRendering(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(200)},
		map[string]any{"id": int64(2), "age": int64(25)},
	))
	db := openHarnessDB(t)
	d := selectiveRenderDialect{Dialect: Postgres(), fail: map[string]bool{"secret_key": true}}

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(d), WithKey("id", "secret_key"), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Err == nil {
		t.Fatal("expected rendering error on failed-keys path")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryRendering) {
		t.Fatalf("category = %v", rep.Results[0].Err)
	}
}
