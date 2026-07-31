package conformance

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/busyminds/gxsql"
)

func runCrossColumnRowInvariants(t *testing.T, cfg Config) {
	t.Helper()
	operators := []struct {
		name string
		exp  gxsql.Expectation
		want int
	}{
		{"equal numeric", gxsql.Column("paid_cents").EqualColumn("invoice_cents"), 3},
		{"not equal numeric", gxsql.Column("paid_cents").NotEqualColumn("invoice_cents"), 3},
		{"less numeric", gxsql.Column("paid_cents").LessThanColumn("invoice_cents"), 4},
		{"less or equal numeric", gxsql.Column("paid_cents").LessOrEqualColumn("invoice_cents"), 3},
		{"greater numeric", gxsql.Column("paid_cents").GreaterThanColumn("invoice_cents"), 3},
		{"greater or equal numeric", gxsql.Column("paid_cents").GreaterOrEqualColumn("invoice_cents"), 2},
		{"equal temporal", gxsql.Column("start_at").EqualColumn("end_at"), 3},
		{"not equal temporal", gxsql.Column("start_at").NotEqualColumn("end_at"), 3},
		{"less temporal", gxsql.Column("start_at").LessThanColumn("end_at"), 3},
		{"less or equal temporal", gxsql.Column("start_at").LessOrEqualColumn("end_at"), 2},
		{"greater temporal", gxsql.Column("start_at").GreaterThanColumn("end_at"), 4},
		{"greater or equal temporal", gxsql.Column("start_at").GreaterOrEqualColumn("end_at"), 3},
	}
	for _, tc := range operators {
		t.Run(tc.name, func(t *testing.T) {
			report, err := gxsql.NewSuite(tc.exp).ValidateTable(
				context.Background(), cfg.DB, cfg.Table, gxsql.WithDialect(cfg.Dialect),
			)
			if err != nil {
				t.Fatalf("ValidateTable: %v", err)
			}
			if got := report.Results[0].FailedCount; got != tc.want {
				t.Fatalf("failed count = %d, want %d; result = %#v", got, tc.want, report.Results[0])
			}
		})
	}

	t.Run("ratio equality is algebraic and rejects zero denominator", func(t *testing.T) {
		report, err := gxsql.NewSuite(gxsql.Int("actual_units").RatioEqual("planned_units", 2)).ValidateTable(
			context.Background(), cfg.DB, cfg.Table, gxsql.WithDialect(cfg.Dialect),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		if got := report.Results[0].FailedCount; got != 2 {
			t.Fatalf("failed count = %d, want 2; result = %#v", got, report.Results[0])
		}
	})

	t.Run("empty scoped population passes", func(t *testing.T) {
		report, err := gxsql.NewSuite(gxsql.Column("paid_cents").EqualColumn("invoice_cents")).ValidateTable(
			context.Background(), cfg.DB, cfg.Table, gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("empty-cross-column", "tenant_id = ?", "nobody")),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		if !report.Results[0].Success || report.Results[0].FailedCount != 0 {
			t.Fatalf("empty scoped result = %#v, want vacuous pass", report.Results[0])
		}
	})

	runCrossColumnGapFixture(t, cfg)
}

// crossColumnGapTable is the dedicated Spec 02 gap fixture. Gap rows (start>end,
// both-NULL, negative/zero ratio, mixed-type; overflow materials reserved on id=7)
// live here so baseline users Total==4 contracts and existing FailedCount
// expectations stay coherent.
func crossColumnGapTable(cfg Config) gxsql.TableRef {
	return gxsql.TableRef{Schema: cfg.Table.Schema, Name: "cross_column_rows"}
}

// gapCoreScope excludes the overflow/mixed-type row (id=7) so ordinary operator
// and ratio checks stay deterministic across engines.
func gapCoreScope() gxsql.Scope {
	return gxsql.TrustedScope("cross-column-core", "id < ?", int64(7))
}

func runCrossColumnGapFixture(t *testing.T, cfg Config) {
	t.Helper()
	table := crossColumnGapTable(cfg)
	scope := gapCoreScope()

	t.Run("gap fixture temporal boundaries include start>end and both-NULL", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.Column("start_at").EqualColumn("end_at"),
			gxsql.Column("start_at").NotEqualColumn("end_at"),
			gxsql.Column("start_at").LessThanColumn("end_at"),
			gxsql.Column("start_at").LessOrEqualColumn("end_at"),
			gxsql.Column("start_at").GreaterThanColumn("end_at"),
			gxsql.Column("start_at").GreaterOrEqualColumn("end_at"),
		).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(scope),
			gxsql.WithKey("id"),
			gxsql.WithFailedKeysCap(0),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}

		// Core rows: 1 start<end, 2 equal, 3 start>end, 4 both-NULL, 5 end NULL, 6 start NULL.
		wantFailed := []int{5, 4, 5, 4, 5, 4}
		wantKeys := [][]gxsql.RowKey{
			{{int64(1)}, {int64(3)}, {int64(4)}, {int64(5)}, {int64(6)}}, // Equal
			{{int64(2)}, {int64(4)}, {int64(5)}, {int64(6)}},             // NotEqual
			{{int64(2)}, {int64(3)}, {int64(4)}, {int64(5)}, {int64(6)}}, // LessThan
			{{int64(3)}, {int64(4)}, {int64(5)}, {int64(6)}},             // LessOrEqual
			{{int64(1)}, {int64(2)}, {int64(4)}, {int64(5)}, {int64(6)}}, // GreaterThan (id 3 passes)
			{{int64(1)}, {int64(4)}, {int64(5)}, {int64(6)}},             // GreaterOrEqual
		}
		for i, want := range wantFailed {
			res := report.Results[i]
			if res.Total != 6 || res.FailedCount != want {
				t.Fatalf("temporal result %d = %#v, want total=6 failed=%d", i, res, want)
			}
			if !reflect.DeepEqual(res.FailedKeys, wantKeys[i]) {
				t.Fatalf("temporal result %d FailedKeys = %#v, want %#v", i, res.FailedKeys, wantKeys[i])
			}
		}
		// Both-NULL (id 4) must appear in every temporal failure set.
		for i, res := range report.Results {
			if !rowKeyPresent(res.FailedKeys, int64(4)) {
				t.Fatalf("temporal result %d missing both-NULL id 4: %#v", i, res.FailedKeys)
			}
		}
		// start>end true path: only GreaterThan/GreaterOrEqual may omit id 3.
		if rowKeyPresent(report.Results[4].FailedKeys, int64(3)) {
			t.Fatalf("GreaterThan must pass start>end row 3; FailedKeys=%#v", report.Results[4].FailedKeys)
		}
		if rowKeyPresent(report.Results[5].FailedKeys, int64(3)) {
			t.Fatalf("GreaterOrEqual must pass start>end row 3; FailedKeys=%#v", report.Results[5].FailedKeys)
		}
	})

	t.Run("gap fixture equal and not equal distinguish keys including both-NULL", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.Column("paid_cents").EqualColumn("invoice_cents"),
			gxsql.Column("paid_cents").NotEqualColumn("invoice_cents"),
		).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(scope),
			gxsql.WithKey("id"),
			gxsql.WithFailedKeysCap(0),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		equalKeys := []gxsql.RowKey{{int64(2)}, {int64(3)}, {int64(4)}, {int64(5)}, {int64(6)}}
		notEqualKeys := []gxsql.RowKey{{int64(1)}, {int64(4)}, {int64(5)}, {int64(6)}}
		if !reflect.DeepEqual(report.Results[0].FailedKeys, equalKeys) {
			t.Fatalf("Equal FailedKeys = %#v, want %#v", report.Results[0].FailedKeys, equalKeys)
		}
		if !reflect.DeepEqual(report.Results[1].FailedKeys, notEqualKeys) {
			t.Fatalf("NotEqual FailedKeys = %#v, want %#v", report.Results[1].FailedKeys, notEqualKeys)
		}
		if reflect.DeepEqual(report.Results[0].FailedKeys, report.Results[1].FailedKeys) {
			t.Fatal("Equal and NotEqual FailedKeys must differ")
		}
		if !rowKeyPresent(report.Results[0].FailedKeys, int64(4)) || !rowKeyPresent(report.Results[1].FailedKeys, int64(4)) {
			t.Fatal("both-NULL numeric row must fail Equal and NotEqual")
		}
	})

	t.Run("gap fixture ratio negative and zero bounds", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.Int("actual_units").RatioEqual("planned_units", -2),
			gxsql.Int("actual_units").RatioEqual("planned_units", 0),
		).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(scope),
			gxsql.WithKey("id"),
			gxsql.WithFailedKeysCap(0),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		// Bound -2: only row 2 (-20 == 10*-2) passes.
		wantNeg := []gxsql.RowKey{{int64(1)}, {int64(3)}, {int64(4)}, {int64(5)}, {int64(6)}}
		// Bound 0: only row 3 (0 == 5*0) passes; row 5 still fails on zero denominator.
		wantZero := []gxsql.RowKey{{int64(1)}, {int64(2)}, {int64(4)}, {int64(5)}, {int64(6)}}
		if !reflect.DeepEqual(report.Results[0].FailedKeys, wantNeg) {
			t.Fatalf("negative-bound FailedKeys = %#v, want %#v", report.Results[0].FailedKeys, wantNeg)
		}
		if report.Results[0].Facts.Ratio == nil || report.Results[0].Facts.Ratio.Bound != -2 {
			t.Fatalf("negative-bound facts = %#v, want bound -2", report.Results[0].Facts.Ratio)
		}
		if !reflect.DeepEqual(report.Results[1].FailedKeys, wantZero) {
			t.Fatalf("zero-bound FailedKeys = %#v, want %#v", report.Results[1].FailedKeys, wantZero)
		}
		if report.Results[1].Facts.Ratio == nil || report.Results[1].Facts.Ratio.Bound != 0 {
			t.Fatalf("zero-bound facts = %#v, want bound 0", report.Results[1].Facts.Ratio)
		}
	})

	t.Run("gap fixture ratio overflow preserves typed database errors", func(t *testing.T) {
		overflowScope := gxsql.TrustedScope("cross-column-overflow", "id = ?", int64(7))
		exp := gxsql.Int("actual_units").RatioEqual("planned_units", 2)
		report, err := gxsql.NewSuite(exp).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(overflowScope),
		)
		if err != nil {
			if !errors.Is(err, gxsql.ErrCategoryDatabase) {
				t.Fatalf("overflow ValidateTable error = %v, want CategoryDatabase", err)
			}
			return
		}
		if len(report.Results) != 1 {
			t.Fatalf("overflow results = %d, want one result", len(report.Results))
		}
		res := report.Results[0]
		if res.Err != nil || res.Success || res.Total != 1 || res.FailedCount != 1 {
			t.Fatalf("overflow result = %#v, want ordinary failed row", res)
		}
	})

	t.Run("gap fixture mixed-type comparison is engine-conditional", func(t *testing.T) {
		mixedScope := gxsql.TrustedScope("cross-column-mixed", "id = ?", int64(7))
		exp := gxsql.Column("label").EqualColumn("amount")

		switch dialectFamily(cfg.Dialect) {
		case "postgres", "duckdb":
			// Deterministic native type error: these engines reject TEXT vs
			// INTEGER comparison without any cast. Assert CategoryDatabase only.
			_, err := gxsql.NewSuite(exp).ValidateTable(
				context.Background(), cfg.DB, table,
				gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(mixedScope),
			)
			if !errors.Is(err, gxsql.ErrCategoryDatabase) {
				t.Fatalf("mixed-type ValidateTable error = %v, want CategoryDatabase", err)
			}
			report, err := gxsql.NewSuite(exp).ValidateTable(
				context.Background(), cfg.DB, table,
				gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(mixedScope), gxsql.ContinueOnError(),
			)
			if err != nil {
				t.Fatalf("ContinueOnError ValidateTable: %v", err)
			}
			if len(report.Results) != 1 || report.Results[0].Success ||
				!errors.Is(report.Results[0].Err, gxsql.ErrCategoryDatabase) {
				t.Fatalf("ContinueOnError mixed-type result = %#v, want CategoryDatabase", report.Results[0])
			}
		case "sqlite":
			// SQLite type affinity compares across storage classes and never
			// raises a native type error for label (TEXT) vs amount (INTEGER).
			// A typed CategoryDatabase assertion therefore cannot be satisfied
			// on SQLite for this pair. Assert the native comparison runs (no
			// harness cast) and Equal fails on value ("abc" != 1).
			report, err := gxsql.NewSuite(exp).ValidateTable(
				context.Background(), cfg.DB, table,
				gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(mixedScope), gxsql.WithKey("id"),
			)
			if err != nil {
				t.Fatalf("ValidateTable: %v", err)
			}
			res := report.Results[0]
			if errors.Is(res.Err, gxsql.ErrCategoryDatabase) {
				t.Fatalf("SQLite affinity must not yield CategoryDatabase for TEXT/INTEGER; got %#v", res)
			}
			if res.Err != nil {
				t.Fatalf("result Err = %v, want nil ordinary evaluation under affinity", res.Err)
			}
			if res.Success || res.Total != 1 || res.FailedCount != 1 {
				t.Fatalf("mixed-type result = %#v, want ordinary Equal failure under SQLite affinity", res)
			}
		case "mysql":
			// MySQL coerces the string operand in a native comparison and does
			// not reject the type pair, so CategoryDatabase is not observable.
			report, err := gxsql.NewSuite(exp).ValidateTable(
				context.Background(), cfg.DB, table,
				gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(mixedScope), gxsql.WithKey("id"),
			)
			if err != nil {
				t.Fatalf("ValidateTable: %v", err)
			}
			res := report.Results[0]
			if errors.Is(res.Err, gxsql.ErrCategoryDatabase) {
				t.Fatalf("MySQL must not yield CategoryDatabase for VARCHAR/BIGINT here; got %#v", res)
			}
			if res.Err != nil {
				t.Fatalf("result Err = %v, want nil ordinary evaluation", res.Err)
			}
			if res.Success || res.Total != 1 || res.FailedCount != 1 {
				t.Fatalf("mixed-type result = %#v, want ordinary Equal failure", res)
			}
		default:
			t.Fatalf("unrecognized dialect %T", cfg.Dialect)
		}
	})
}

func rowKeyPresent(keys []gxsql.RowKey, id int64) bool {
	for _, key := range keys {
		if len(key) == 1 && key[0] == id {
			return true
		}
	}
	return false
}

// dialectFamily fingerprints the concrete Dialect without relying on an
// exported Name method. QuoteIdent + Placeholder + StringLength uniquely
// identify the four supported engines.
func dialectFamily(d gxsql.Dialect) string {
	quoted, err := d.QuoteIdent("x")
	if err != nil {
		return "unknown"
	}
	switch {
	case d.Placeholder(1) == "?" && d.StringLength("x") == "LENGTH(x)":
		return "sqlite"
	case d.Placeholder(1) == "?" && quoted == "`x`":
		return "mysql"
	case d.Placeholder(1) == "$1" && d.StringLength("x") == "LENGTH(x)":
		return "duckdb"
	case d.Placeholder(1) == "$1":
		return "postgres"
	default:
		return "unknown"
	}
}
