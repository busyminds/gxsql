package gxsql

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCompositeUniquePreflightRejectsArityAndIdentifiers(t *testing.T) {
	setHarnessData(t, harnessUsers())
	cases := []struct {
		name string
		exp  Expectation
	}{
		{name: "zero", exp: Columns().Unique()},
		{name: "one", exp: Columns("tenant_id").Unique()},
		{name: "empty", exp: Columns("tenant_id", "").Unique()},
		{name: "invalid", exp: Columns("tenant_id", "order-id").Unique()},
		{name: "duplicate", exp: Columns("tenant_id", "tenant_id").Unique()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openCountingHarnessDB(t)
			_, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), db, Table("users"), WithDialect(Postgres()),
			)
			if err == nil {
				t.Fatal("expected preflight error")
			}
			var pf *PreflightErrors
			if !errors.As(err, &pf) {
				t.Fatalf("got %T (%v), want *PreflightErrors", err, err)
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) {
				t.Fatalf("category = %v", err)
			}
			if db.queries != 0 {
				t.Fatalf("queries = %d, want 0 before SQL", db.queries)
			}
		})
	}
}

func TestCompositeUniqueEmptyPopulationPasses(t *testing.T) {
	setHarnessData(t, harnessUsers())
	db := openHarnessDB(t)

	rep, err := NewSuite(Columns("tenant_id", "order_id").Unique()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.Total != 0 || res.FailedCount != 0 {
		t.Fatalf("got %+v, want vacuous pass", res)
	}
	assertCompositeUniqueResultShape(t, res, []string{"tenant_id", "order_id"})
}

func TestCompositeUniqueOneDuplicateGroupFailsEveryRow(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(2), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(3), "tenant_id": "t1", "order_id": "o2"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Columns("tenant_id", "order_id").Unique()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success {
		t.Fatal("expected failure")
	}
	if res.Total != 3 || res.FailedCount != 2 {
		t.Fatalf("Total=%d FailedCount=%d, want 3 and 2", res.Total, res.FailedCount)
	}
	assertCompositeUniqueResultShape(t, res, []string{"tenant_id", "order_id"})
}

func TestCompositeUniqueMultipleDuplicateGroupsCountEveryDuplicateRow(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(2), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "order_id": "o9"},
		map[string]any{"id": int64(4), "tenant_id": "t2", "order_id": "o9"},
		map[string]any{"id": int64(5), "tenant_id": "t2", "order_id": "o9"},
		map[string]any{"id": int64(6), "tenant_id": "t3", "order_id": "unique"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Columns("tenant_id", "order_id").Unique()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success {
		t.Fatal("expected failure")
	}
	if res.Total != 6 || res.FailedCount != 5 {
		t.Fatalf("Total=%d FailedCount=%d, want 6 and 5 (all duplicate rows)", res.Total, res.FailedCount)
	}
}

func TestCompositeUniqueIgnoresNULLContainingTuples(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "t1", "order_id": nil},
		map[string]any{"id": int64(2), "tenant_id": "t1", "order_id": nil},
		map[string]any{"id": int64(3), "tenant_id": nil, "order_id": "o1"},
		map[string]any{"id": int64(4), "tenant_id": nil, "order_id": "o1"},
		map[string]any{"id": int64(5), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(6), "tenant_id": "t2", "order_id": "o2"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Columns("tenant_id", "order_id").Unique()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.FailedCount != 0 {
		t.Fatalf("got %+v, want pass while ignoring NULL-containing tuples", res)
	}
	if res.Total != 6 {
		t.Fatalf("Total=%d, want 6", res.Total)
	}
}

func TestCompositeUniqueNULLIgnoredBesideDuplicateCompleteTuples(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(2), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(3), "tenant_id": "t1", "order_id": nil},
		map[string]any{"id": int64(4), "tenant_id": nil, "order_id": "o1"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Columns("tenant_id", "order_id").Unique()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success || res.Total != 4 || res.FailedCount != 2 {
		t.Fatalf("got %+v, want FailedCount 2 for complete duplicate rows only", res)
	}
}

func TestCompositeUniqueScopedPopulationIgnoresOutOfScopeDuplicates(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("region", "eu",
		map[string]any{"id": int64(1), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(2), "tenant_id": "t2", "order_id": "o2"},
		map[string]any{"id": int64(3), "region": "us", "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(4), "region": "us", "tenant_id": "t1", "order_id": "o1"},
	))
	scope := mustTestScope(t, "region = ?", "eu")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Columns("tenant_id", "order_id").Unique(), scope, func(o *evalOptions) {
		o.sampleCap = 0
	})
	if !res.Success || res.Total != 2 || res.FailedCount != 0 {
		t.Fatalf("got %+v, want scoped pass ignoring out-of-scope duplicates", res)
	}
	assertCompositeUniqueResultShape(t, res, []string{"tenant_id", "order_id"})
	if len(db.queries) != 2 {
		t.Fatalf("queries=%d, want total and failure counts", len(db.queries))
	}
	if got := db.queries[1].args; len(got) != 2 || got[0] != "eu" || got[1] != "eu" {
		t.Fatalf("failure args=%v, want [eu eu]", got)
	}
}

func TestCompositeUniqueScopedDuplicateGroupAndPlaceholderOrdering(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("region", "eu",
		map[string]any{"id": int64(1), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(2), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(3), "region": "us", "tenant_id": "t1", "order_id": "o1"},
	))
	scope := mustTestScope(t, "region = ?", "eu")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Columns("tenant_id", "order_id").Unique(), scope, func(o *evalOptions) {
		o.sampleCap = 0
	})
	if res.Success || res.Total != 2 || res.FailedCount != 2 {
		t.Fatalf("got %+v, want scoped duplicate failure for both rows", res)
	}

	failure := db.queries[1]
	if len(failure.args) != 2 || failure.args[0] != "eu" || failure.args[1] != "eu" {
		t.Fatalf("failure args=%v, want [eu eu]", failure.args)
	}
	if !strings.Contains(failure.text, `(region = $1) AND (`) {
		t.Fatalf("failure query lacks outer scope: %q", failure.text)
	}
	if !strings.Contains(failure.text, `WHERE (region = $2)`) {
		t.Fatalf("failure query lacks offset inner scope: %q", failure.text)
	}
	if !strings.Contains(failure.text, `("tenant_id", "order_id") IN (SELECT "tenant_id", "order_id"`) {
		t.Fatalf("failure query lacks composite tuple predicate: %q", failure.text)
	}
}

func TestCompositeUniqueDialectRenderingAndPlaceholders(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "order_id": "o1"},
		map[string]any{"id": int64(2), "order_id": "o1"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")

	tests := []struct {
		name        string
		dialect     Dialect
		outerScope  string
		innerScope  string
		tupleSelect string
	}{
		{
			name:        "postgres",
			dialect:     Postgres(),
			outerScope:  `(tenant_id = $1) AND (`,
			innerScope:  `WHERE (tenant_id = $2)`,
			tupleSelect: `("tenant_id", "order_id") IN (SELECT "tenant_id", "order_id"`,
		},
		{
			name:        "sqlite",
			dialect:     SQLite(),
			outerScope:  `(tenant_id = ?) AND (`,
			innerScope:  `WHERE (tenant_id = ?)`,
			tupleSelect: `("tenant_id", "order_id") IN (SELECT "tenant_id", "order_id"`,
		},
		{
			name:        "duckdb",
			dialect:     DuckDB(),
			outerScope:  `(tenant_id = $1) AND (`,
			innerScope:  `WHERE (tenant_id = $2)`,
			tupleSelect: `("tenant_id", "order_id") IN (SELECT "tenant_id", "order_id"`,
		},
		{
			name:        "mysql",
			dialect:     MySQL(),
			outerScope:  `(tenant_id = ?) AND (`,
			innerScope:  `WHERE (tenant_id = ?)`,
			tupleSelect: "(`tenant_id`, `order_id`) IN (SELECT `tenant_id`, `order_id`",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openRecordingHarnessDB(t)
			res := evalPerRowWithScope(t, db, Columns("tenant_id", "order_id").Unique(), scope, func(o *evalOptions) {
				o.dialect = tc.dialect
				o.sampleCap = 0
			})
			if res.Success || res.Total != 2 || res.FailedCount != 2 {
				t.Fatalf("result = %#v, want scoped composite failure", res)
			}
			failure := db.queries[1]
			if len(failure.args) != 2 || failure.args[0] != "t1" || failure.args[1] != "t1" {
				t.Fatalf("failure args = %#v, want [t1 t1]", failure.args)
			}
			if !strings.Contains(failure.text, tc.outerScope) {
				t.Fatalf("missing outer scope %q in %q", tc.outerScope, failure.text)
			}
			if !strings.Contains(failure.text, tc.innerScope) {
				t.Fatalf("missing inner scope %q in %q", tc.innerScope, failure.text)
			}
			if !strings.Contains(failure.text, tc.tupleSelect) {
				t.Fatalf("missing tuple select %q in %q", tc.tupleSelect, failure.text)
			}
		})
	}
}

func TestCompositeUniqueCapsAndFailedKeys(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(2), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(3), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(4), "tenant_id": "t2", "order_id": "o2"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Columns("tenant_id", "order_id").Unique()).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithKey("id"),
		WithSampleCap(2),
		WithFailedKeysCap(2),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success || res.FailedCount != 3 {
		t.Fatalf("got %+v, want FailedCount 3", res)
	}
	if len(res.SampleValues) != 2 {
		t.Fatalf("SampleValues len = %d, want 2 (sample cap)", len(res.SampleValues))
	}
	wantKeys := []RowKey{{int64(1)}, {int64(2)}}
	if !reflect.DeepEqual(res.FailedKeys, wantKeys) {
		t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, wantKeys)
	}
	assertCompositeUniqueResultShape(t, res, []string{"tenant_id", "order_id"})
}

func TestCompositeUniquePreservesSingleColumnUniqueKind(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "a@b.com"},
		map[string]any{"id": int64(2), "email": "b@b.com"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		Column("email").Unique(),
		Columns("id", "email").Unique(),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Kind != KindUnique || rep.Results[0].Column != "email" {
		t.Fatalf("single-column unique mutated: %#v", rep.Results[0])
	}
	assertCompositeUniqueResultShape(t, rep.Results[1], []string{"id", "email"})
}

func TestCompositeUniqueToleranceEligible(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "t1", "order_id": "o1"},
		map[string]any{"id": int64(2), "tenant_id": "t1", "order_id": "o1"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(WithMaxFailedCount(2, Columns("tenant_id", "order_id").Unique())).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || !res.Tolerated || res.FailedCount != 2 {
		t.Fatalf("got %+v, want tolerated success with raw FailedCount 2", res)
	}
	assertCompositeUniqueResultShape(t, res, []string{"tenant_id", "order_id"})
}

func assertCompositeUniqueResultShape(t *testing.T, res Result, wantKeys []string) {
	t.Helper()
	if res.Kind != KindCompositeUnique {
		t.Fatalf("Kind = %q, want %q", res.Kind, KindCompositeUnique)
	}
	if res.Column != "" {
		t.Fatalf("Column = %q, want empty", res.Column)
	}
	if !reflect.DeepEqual(res.Facts.KeyColumns, wantKeys) {
		t.Fatalf("KeyColumns = %#v, want %#v", res.Facts.KeyColumns, wantKeys)
	}
}

func TestCompositeUniqueConfigErrorContinueOnError(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "a@b.com"},
	))
	counter := openCountingHarnessDB(t)

	rep, err := NewSuite(
		WithID("bad-composite", Columns("tenant_id").Unique()),
		WithID("good-unique", Column("email").Unique()),
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
	if rep.Results[0].Success || rep.Results[0].Err == nil {
		t.Fatal("index 0 should be configuration failure slot")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryInvalidConfig) {
		t.Fatalf("index 0 category = %v", rep.Results[0].Err)
	}
	if !rep.Results[1].Success || rep.Results[1].Kind != KindUnique {
		t.Fatalf("index 1 should run valid unique, got %+v", rep.Results[1])
	}
	if counter.queries == 0 {
		t.Fatal("valid expectation should execute SQL under ContinueOnError")
	}
}

type compositeNonRelationalDialect struct {
	Dialect
}

func TestCompositeUniqueUnsupportedDialectBeforeSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "t1", "order_id": "o1"},
	))
	counter := openCountingHarnessDB(t)
	d := compositeNonRelationalDialect{Dialect: Postgres()}

	_, err := NewSuite(Columns("tenant_id", "order_id").Unique()).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(d),
	)
	if err == nil {
		t.Fatal("expected preflight error for unsupported relational dialect")
	}
	var pf *PreflightErrors
	if !errors.As(err, &pf) {
		t.Fatalf("got %T (%v), want *PreflightErrors", err, err)
	}
	if !errors.Is(err, ErrCategoryUnsupported) {
		t.Fatalf("category = %v", err)
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before SQL", counter.queries)
	}
}

func TestCompositeUniqueUnsupportedDialectContinueOnError(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "a@b.com", "tenant_id": "t1", "order_id": "o1"},
	))
	counter := openCountingHarnessDB(t)
	d := compositeNonRelationalDialect{Dialect: Postgres()}

	rep, err := NewSuite(
		WithID("composite", Columns("tenant_id", "order_id").Unique()),
		WithID("unique", Column("email").Unique()),
	).ValidateTable(
		context.Background(), counter, Table("users"),
		WithDialect(d), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results len = %d", len(rep.Results))
	}
	if rep.Results[0].Success || rep.Results[0].Err == nil {
		t.Fatal("composite should attach unsupported dialect config slot")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryUnsupported) {
		t.Fatalf("composite category = %v", rep.Results[0].Err)
	}
	if !rep.Results[1].Success || rep.Results[1].Kind != KindUnique {
		t.Fatalf("single-column unique should still run, got %+v", rep.Results[1])
	}
	if counter.queries == 0 {
		t.Fatal("valid non-relational expectation should execute SQL")
	}
}

func TestCompositeUniqueUsesRowDenominator(t *testing.T) {
	if !usesRowDenominator(Columns("tenant_id", "order_id").Unique()) {
		t.Fatal("composite unique should use row denominator")
	}
	if !usesRowDenominator(WithMaxFailedCount(1, Columns("a", "b").Unique())) {
		t.Fatal("tolerated composite unique should still use row denominator")
	}
}
