package gxsql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

type countingDB struct {
	DB
	queries int
}

func (c *countingDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	c.queries++
	return c.DB.QueryContext(ctx, query, args...)
}

func (c *countingDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	c.queries++
	return c.DB.QueryRowContext(ctx, query, args...)
}

func openCountingHarnessDB(t *testing.T) *countingDB {
	t.Helper()
	return &countingDB{DB: openHarnessDB(t)}
}

func TestWithIDBlankRejectedBeforeSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(WithID(" ", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected blank id configuration error")
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before SQL on config failure", counter.queries)
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v", err)
	}
}

func TestMissingResultIDAllowedWithoutWithID(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(rep.Results))
	}
	res := rep.Results[0]
	if res.ID != "" {
		t.Fatalf("ID = %q, want empty when WithID is not used", res.ID)
	}
	if res.Kind != KindBetween {
		t.Fatalf("Kind = %q, want %q", res.Kind, KindBetween)
	}

	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(dto.Results) != 1 {
		t.Fatalf("exported results len = %d", len(dto.Results))
	}
	if dto.Results[0].ID != "" {
		t.Fatalf("exported id = %q, want empty (omitempty)", dto.Results[0].ID)
	}
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"id"`)) {
		t.Fatalf("exported JSON should omit id when unset: %s", data)
	}
}

func TestWithIDDuplicateRejectedBeforeSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
	))
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(
		WithID("dup", Int("age").Between(0, 120)),
		WithID("dup", String("email").NotEmpty()),
	).ValidateTable(context.Background(), counter, Table("users"), WithDialect(Postgres()))
	if err == nil {
		t.Fatal("expected duplicate id configuration error")
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before SQL on config failure", counter.queries)
	}
	var pf *PreflightErrors
	if !errors.As(err, &pf) {
		t.Fatalf("got %T", err)
	}
	if len(pf.Issues) < 2 {
		t.Fatalf("issues = %d, want at least 2 for duplicate pair", len(pf.Issues))
	}
}

func TestStableIDAndKindDespiteDisplayWording(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		WithID("age-range", Int("age").Between(0, 120)),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.ID != "age-range" {
		t.Fatalf("ID = %q, want stable caller id", res.ID)
	}
	if res.Kind != KindBetween {
		t.Fatalf("Kind = %q, want %q", res.Kind, KindBetween)
	}
	if res.Name == "" {
		t.Fatal("display Name should remain populated")
	}
}

func TestResultIDAndKindStableWhenDisplayNameChanges(t *testing.T) {
	run := func(t *testing.T, rows ...map[string]any) Result {
		t.Helper()
		setHarnessData(t, harnessUsers(rows...))
		db := openHarnessDB(t)
		rep, err := NewSuite(
			WithID("rows", RowCount().Equal(1)),
		).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
		if err != nil {
			t.Fatal(err)
		}
		return rep.Results[0]
	}

	passRes := run(t, map[string]any{"id": int64(1)})
	failRes := run(t,
		map[string]any{"id": int64(1)},
		map[string]any{"id": int64(2)},
	)

	if passRes.ID != "rows" || failRes.ID != "rows" {
		t.Fatalf("IDs = [%q, %q], want stable rows", passRes.ID, failRes.ID)
	}
	if passRes.Kind != KindRowCountEqual || failRes.Kind != KindRowCountEqual {
		t.Fatalf("Kinds = [%q, %q], want %q", passRes.Kind, failRes.Kind, KindRowCountEqual)
	}
	if passRes.Name == failRes.Name {
		t.Fatalf("display names should differ when observations differ: %q", passRes.Name)
	}
	if passRes.Name == "" || failRes.Name == "" {
		t.Fatal("display Name should remain populated")
	}
}

func TestValidateTableConcurrentReuseWithImmutableConfiguration(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(30)},
	))
	db := openHarnessDB(t)
	suite := NewSuite(WithID("age", Int("age").Between(0, 120)))

	const workers = 8
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			rep, err := suite.ValidateTable(
				context.Background(), db, Table("users"), WithDialect(Postgres()),
			)
			if err != nil {
				errCh <- err
				return
			}
			if !rep.OK() {
				errCh <- fmt.Errorf("unexpected failure: %+v", rep.Results)
				return
			}
			errCh <- nil
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTableLevelResultMarksUnavailableDenominator(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1)},
		map[string]any{"id": int64(2)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(WithID("rows", RowCount().Equal(2))).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.RowDenominator != RowDenominatorUnavailable {
		t.Fatalf("RowDenominator = %q, want unavailable", res.RowDenominator)
	}
	if res.Total != 0 {
		t.Fatalf("Total = %d, want 0 for table-level check", res.Total)
	}
	if res.FailedPercent != 0 {
		t.Fatalf("FailedPercent = %v, want 0 when denominator unavailable", res.FailedPercent)
	}
	if res.Facts.ObservedCount == nil || *res.Facts.ObservedCount != 2 {
		t.Fatalf("Facts.ObservedCount = %v, want 2 in machine facts", res.Facts.ObservedCount)
	}
	if res.Kind != KindRowCountEqual {
		t.Fatalf("Kind = %q", res.Kind)
	}
}

func TestPerRowResultMarksAvailableDenominator(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(200)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.RowDenominator != RowDenominatorAvailable {
		t.Fatalf("RowDenominator = %q, want available", res.RowDenominator)
	}
	if res.Total != 2 {
		t.Fatalf("Total = %d", res.Total)
	}
}

func TestCollectedConfigurationErrorsBeforeSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active"},
	))
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(
		Column("status").In(),
		WithID("", String("status").NotEmpty()),
	).ValidateTable(context.Background(), counter, Table("users"), WithDialect(Postgres()))
	if err == nil {
		t.Fatal("expected collected configuration errors")
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0", counter.queries)
	}
	var pf *PreflightErrors
	if !errors.As(err, &pf) {
		t.Fatalf("got %T", err)
	}
	if len(pf.Issues) < 2 {
		t.Fatalf("issues = %d, want multiple collected config errors", len(pf.Issues))
	}
}

func TestContinueOnErrorConfigSlotsPreserveDeclarationOrder(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
	))
	counter := openCountingHarnessDB(t)

	rep, err := NewSuite(
		WithID("bad", Column("status").In()),
		WithID("good", Int("age").Between(0, 120)),
		WithID("also-bad", Column("status").NotIn("x", nil)),
	).ValidateTable(
		context.Background(), counter, Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results len = %d", len(rep.Results))
	}
	if rep.Results[0].Success || rep.Results[0].Err == nil {
		t.Fatal("index 0 should be configuration failure slot")
	}
	if !rep.Results[1].Success {
		t.Fatal("index 1 valid expectation should run and pass")
	}
	if rep.Results[1].Kind != KindBetween {
		t.Fatalf("index 1 kind = %q", rep.Results[1].Kind)
	}
	if rep.Results[2].Success || rep.Results[2].Err == nil {
		t.Fatal("index 2 should be configuration failure slot")
	}
	if counter.queries == 0 {
		t.Fatal("valid expectation should execute SQL under ContinueOnError")
	}
}

func TestCategorizedErrorSupportsErrorsIsAndAs(t *testing.T) {
	cfgErr := newConfigError(errors.New("bad config"))
	if !errors.Is(cfgErr, ErrCategoryInvalidConfig) {
		t.Fatal("expected errors.Is for invalid_config")
	}
	var ce *CategorizedError
	if !errors.As(cfgErr, &ce) {
		t.Fatal("expected errors.As *CategorizedError")
	}
	if ce.Category != CategoryInvalidConfig {
		t.Fatalf("Category = %q", ce.Category)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	execErr := categorizeExecutionError(ctx, errors.New("db down"))
	if !errors.Is(execErr, ErrCategoryContext) {
		t.Fatal("expected context category when ctx cancelled")
	}
	if !errors.Is(execErr, context.Canceled) {
		t.Fatal("expected errors.Is context.Canceled through categorized error")
	}
}

func TestCustomExpectationUsesExplicitCustomKind(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openHarnessDB(t)

	custom := customTestExpectation{name: "custom check"}
	rep, err := NewSuite(custom).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Kind != KindCustom {
		t.Fatalf("Kind = %q, want custom", rep.Results[0].Kind)
	}
}

type customTestExpectation struct {
	name string
}

func (e customTestExpectation) Name() string { return e.name }

func (e customTestExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	return Result{Name: e.name, Success: true}, nil
}

func TestValidationPolicyErrUnchangedForDataFailures(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(200)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(WithID("age", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Err() == nil {
		t.Fatal("expected validation policy error")
	}
	var ve *ValidationError
	if !errors.As(rep.Err(), &ve) {
		t.Fatalf("got %T", rep.Err())
	}
	if ve.Report.Results[0].ID != "age" {
		t.Fatalf("ID = %q", ve.Report.Results[0].ID)
	}
}
