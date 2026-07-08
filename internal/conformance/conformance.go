// Package conformance contains the shared real-engine contract tests for gxsql
// dialects. Database setup and driver lifecycle stay with each integration
// package; this runner only uses the narrow gxsql.DB interface.
package conformance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/busyminds/gxsql"
)

// Config supplies an engine fixture to Run. Table and EmptyTable must expose
// the same columns: id, name, age, score, nullable, and payload.
type Config struct {
	DB          gxsql.DB
	Dialect     gxsql.Dialect
	Table       gxsql.TableRef
	EmptyTable  gxsql.TableRef
	Transaction func(context.Context) (gxsql.DB, func() error, error)
}

// Run executes the same behavior contract against any database/sql driver and
// gxsql dialect. Each subtest names the capability it exercises so dialect
// authors can identify a compatibility gap without reading SQL traces.
func Run(t *testing.T, cfg Config) {
	t.Helper()
	if cfg.DB == nil {
		t.Fatal("conformance: DB is required")
	}
	if cfg.Dialect == nil {
		t.Fatal("conformance: dialect is required")
	}

	t.Run("identifiers/schema qualification and rejection", func(t *testing.T) {
		if _, err := cfg.Dialect.QuoteIdent("valid_name"); err != nil {
			t.Fatalf("valid identifier rejected: %v", err)
		}
		if _, err := cfg.Dialect.QuoteIdent("bad-name"); err == nil {
			t.Fatal("invalid identifier accepted")
		}
		_, err := gxsql.NewSuite(gxsql.RowCount().GreaterOrEqual(1)).ValidateTable(
			context.Background(), cfg.DB, gxsql.Table("bad-name"), gxsql.WithDialect(cfg.Dialect))
		if !errors.Is(err, gxsql.ErrCategoryRendering) {
			t.Fatalf("invalid table error category = %v, want rendering", err)
		}

	})
	t.Run("placeholder numbering and bound values", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.Int("age").Between(0, 120),
			gxsql.Column("name").In("alice", "zed"),
		).ValidateTable(context.Background(), cfg.DB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		if got := report.Results[0].FailedCount; got != 2 {
			t.Fatalf("age failed count = %d, want 2", got)
		}
		if report.Results[1].FailedCount != 1 {
			t.Fatalf("name membership failed count = %d, want 1", report.Results[1].FailedCount)
		}
	})

	t.Run("null scans and text/byte sample values", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.Column("nullable").NotNull(),
			gxsql.String("name").NotEmpty(),
		).ValidateTable(context.Background(), cfg.DB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithSampleCap(10))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		if report.Results[0].FailedCount != 1 || report.Results[1].FailedCount != 1 {
			t.Fatalf("null/text failures = %d/%d, want 1/1", report.Results[0].FailedCount, report.Results[1].FailedCount)
		}
		for _, value := range report.Results[1].SampleValues {
			switch value.(type) {
			case string, []byte:
			default:
				t.Fatalf("sample value type %T is neither string nor []byte", value)
			}
		}
	})

	t.Run("single and composite failed-row keys", func(t *testing.T) {
		suite := gxsql.NewSuite(gxsql.Column("name").Unique())
		for _, tc := range []struct {
			name string
			keys []string
		}{
			{name: "single", keys: []string{"id"}},
			{name: "composite", keys: []string{"id", "name"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				report, err := suite.ValidateTable(context.Background(), cfg.DB, cfg.Table,
					gxsql.WithDialect(cfg.Dialect), gxsql.WithKey(tc.keys...), gxsql.WithFailedKeysCap(0))
				if err != nil {
					t.Fatalf("ValidateTable: %v", err)
				}
				if got := report.Results[0].FailedCount; got != 2 {
					t.Fatalf("duplicate failed count = %d, want 2", got)
				}
				if got := len(report.Results[0].FailedKeys); got != 2 {
					t.Fatalf("failed keys = %d, want 2", got)
				}
			})
		}
	})

	t.Run("deterministic ordering and diagnostic limits", func(t *testing.T) {
		report, err := gxsql.NewSuite(gxsql.Int("age").GreaterOrEqual(0)).ValidateTable(
			context.Background(), cfg.DB, cfg.Table, gxsql.WithDialect(cfg.Dialect),
			gxsql.WithKey("id"), gxsql.WithSampleCap(1), gxsql.WithFailedKeysCap(1))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		if res.FailedCount != 1 || len(res.SampleValues) != 1 || len(res.FailedKeys) != 1 {
			t.Fatalf("counts/caps = %d/%d/%d, want 1/1/1", res.FailedCount, len(res.SampleValues), len(res.FailedKeys))
		}
		if got, want := res.FailedKeys[0][0], int64(2); got != want {
			t.Fatalf("first failed key = %#v, want %d", got, want)
		}
	})

	t.Run("empty targets and expectation categories", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.RowCount().Equal(0),
			gxsql.Column("name").NotNull(),
			gxsql.String("name").LenBetween(1, 10),
		).ValidateTable(context.Background(), cfg.DB, cfg.EmptyTable,
			gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		for i, res := range report.Results {
			if !res.Success || res.Err != nil {
				t.Fatalf("result %d = %#v, want vacuous success", i, res)
			}
		}
	})

	t.Run("context cancellation and deadline", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := gxsql.NewSuite(gxsql.RowCount().GreaterOrEqual(1)).ValidateTable(
			ctx, cfg.DB, cfg.Table, gxsql.WithDialect(cfg.Dialect))
		if !errors.Is(err, gxsql.ErrCategoryContext) {
			t.Fatalf("canceled context error = %v, want context category", err)
		}
		deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer deadlineCancel()
		time.Sleep(time.Millisecond)
		_, err = gxsql.NewSuite(gxsql.RowCount().GreaterOrEqual(1)).ValidateTable(
			deadlineCtx, cfg.DB, cfg.Table, gxsql.WithDialect(cfg.Dialect))
		if !errors.Is(err, gxsql.ErrCategoryContext) {
			t.Fatalf("deadline error = %v, want context category", err)
		}
	})

	t.Run("database and scan errors", func(t *testing.T) {
		_, err := gxsql.NewSuite(gxsql.RowCount().GreaterOrEqual(1)).ValidateTable(
			context.Background(), cfg.DB, gxsql.Table("missing_table"), gxsql.WithDialect(cfg.Dialect))
		if !errors.Is(err, gxsql.ErrCategoryDatabase) {
			t.Fatalf("database error = %v, want database category", err)
		}
		rows, err := cfg.DB.QueryContext(context.Background(), "SELECT 'not-an-integer'")
		if err != nil {
			t.Fatalf("scan fixture query: %v", err)
		}
		if !rows.Next() {
			_ = rows.Close()
			t.Fatal("scan fixture returned no rows")
		}
		var value int
		if err := rows.Scan(&value); err == nil {
			t.Fatal("incompatible scan unexpectedly succeeded")
		}
		_ = rows.Close()
	})

	t.Run("ContinueOnError preserves later results", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.RowCount().GreaterOrEqual(1),
			gxsql.RowCount().GreaterOrEqual(1),
		).ValidateTable(context.Background(), cfg.DB, gxsql.Table("missing_table"),
			gxsql.WithDialect(cfg.Dialect), gxsql.ContinueOnError())
		if err != nil {
			t.Fatalf("ContinueOnError: %v", err)
		}
		if len(report.Results) != 2 || report.Results[0].Err == nil || report.Results[1].Err == nil {
			t.Fatalf("results = %#v, want per-result execution failures", report.Results)
		}
	})

	if cfg.Transaction != nil {
		t.Run("narrow DB handles including transactions", func(t *testing.T) {
			tx, rollback, err := cfg.Transaction(context.Background())
			if err != nil {
				t.Fatalf("begin transaction: %v", err)
			}
			defer func() { _ = rollback() }()
			report, err := gxsql.NewSuite(gxsql.RowCount().GreaterOrEqual(1)).ValidateTable(
				context.Background(), tx, cfg.Table, gxsql.WithDialect(cfg.Dialect))
			if err != nil {
				t.Fatalf("ValidateTable on transaction: %v", err)
			}
			if !report.Results[0].Success {
				t.Fatalf("transaction result = %#v", report.Results[0])
			}
		})
	} else {
		t.Log("narrow DB handles including transactions: transaction callback not supplied")
	}

}
