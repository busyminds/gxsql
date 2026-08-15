// Package conformance contains the shared real-engine contract tests for gxsql
// dialects. Database setup and driver lifecycle stay with each integration
// package; this runner only uses the narrow gxsql.DB interface.
package conformance

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/busyminds/gxsql"
)

type recordedQuery struct {
	text string
	args []any
}

type recordingDB struct {
	gxsql.DB
	queries []recordedQuery
}

func (db *recordingDB) record(query string, args ...any) {
	db.queries = append(db.queries, recordedQuery{
		text: query,
		args: append([]any(nil), args...),
	})
}

func (db *recordingDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	db.record(query, args...)
	return db.DB.QueryContext(ctx, query, args...)
}

func (db *recordingDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	db.record(query, args...)
	return db.DB.QueryRowContext(ctx, query, args...)
}

func equalScopeValue(got, want any) bool {
	gotTime, gotIsTime := got.(time.Time)
	wantTime, wantIsTime := want.(time.Time)
	if gotIsTime || wantIsTime {
		return gotIsTime && wantIsTime && gotTime.Equal(wantTime)
	}
	return reflect.DeepEqual(got, want)
}

func assertScopedQueries(t *testing.T, db *recordingDB, scopeColumn string, timeWindow bool, expectedScope ...any) {
	t.Helper()
	if len(db.queries) == 0 {
		t.Fatal("scoped query list is empty")
	}
	scopeColumn = strings.ToLower(scopeColumn)
	for i, record := range db.queries {
		query := strings.ToLower(record.text)
		if !strings.Contains(query, "where") || !strings.Contains(query, scopeColumn) {
			t.Fatalf("scoped query %d = %q, want WHERE and %s scope", i, record.text, scopeColumn)
		}
		if timeWindow && (!strings.Contains(query, "event_at >=") || !strings.Contains(query, "event_at <")) {
			t.Fatalf("scoped time query %d = %q, want event_at >= and event_at <", i, record.text)
		}
		if len(record.args) < len(expectedScope) {
			t.Fatalf("scoped query %d args = %#v, want leading scope args %#v", i, record.args, expectedScope)
		}
		for argIndex, want := range expectedScope {
			if !equalScopeValue(record.args[argIndex], want) {
				t.Fatalf("scoped query %d args = %#v, want leading scope args %#v", i, record.args, expectedScope)
			}
		}
	}
}

// Config supplies an engine fixture to Run. Table and EmptyTable must expose
// the baseline columns plus paid_cents, invoice_cents, start_at, end_at,
// actual_units, and planned_units. ParentTable is the schema-qualified
// customers parent used by relational key integrity checks.
type Config struct {
	DB          gxsql.DB
	Dialect     gxsql.Dialect
	Table       gxsql.TableRef
	EmptyTable  gxsql.TableRef
	ParentTable gxsql.TableRef
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

	t.Run("scoped tenant validation", func(t *testing.T) {
		scope := gxsql.TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.String("name").NotEmpty(),
			gxsql.Column("name").Unique(),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope), gxsql.WithKey("id"))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertScopedQueries(t, db, "tenant_id", false, "tenant-a")
		if report.ScopeID != "tenant-a" || len(report.Results) != 2 {
			t.Fatalf("scope/results = %q/%d, want tenant-a/2", report.ScopeID, len(report.Results))
		}
		perRow := report.Results[0]
		if perRow.RowDenominator != gxsql.RowDenominatorAvailable ||
			perRow.Total != 2 || perRow.FailedCount != 1 || perRow.FailedPercent != 50 {
			t.Fatalf("tenant per-row result = %#v, want total=2 failed=1 percent=50", perRow)
		}
		unique := report.Results[1]
		if unique.RowDenominator != gxsql.RowDenominatorAvailable ||
			unique.Total != 2 || unique.FailedCount != 0 || unique.FailedPercent != 0 {
			t.Fatalf("tenant unique result = %#v, want total=2 failed=0 percent=0", unique)
		}
	})
	t.Run("scoped batch counts", func(t *testing.T) {
		scope := gxsql.TrustedScope("batch-2", "batch_id = ?", int64(2))
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.RowCount().Equal(2),
			gxsql.Column("name").DistinctCount().Equal(2),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertScopedQueries(t, db, "batch_id", false, int64(2))
		if report.ScopeID != "batch-2" || len(report.Results) != 2 {
			t.Fatalf("scope/results = %q/%d, want batch-2/2", report.ScopeID, len(report.Results))
		}
		for i, res := range report.Results {
			if !res.Success || res.Err != nil || res.RowDenominator != gxsql.RowDenominatorUnavailable ||
				res.Total != 0 || res.FailedCount != 0 || res.FailedPercent != 0 ||
				res.Facts.ObservedCount == nil || *res.Facts.ObservedCount != 2 {
				t.Fatalf("batch result %d = %#v, want observed count 2", i, res)
			}
		}
	})

	t.Run("scoped half-open event window", func(t *testing.T) {
		start := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2025, time.January, 3, 0, 0, 0, 0, time.UTC)
		scope := gxsql.TrustedScope("event-window", "event_at >= ? AND event_at < ?", start, end)
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.RowCount().Equal(2),
			gxsql.Float("score").AverageBetween(2, 2),
			gxsql.Int("age").GreaterOrEqual(0),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope),
			gxsql.CaptureQueryDiagnostics())
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertScopedQueries(t, db, "event_at", true, start, end)
		if report.ScopeID != "event-window" || len(report.Results) != 3 {
			t.Fatalf("scope/results = %q/%d, want event-window/3", report.ScopeID, len(report.Results))
		}
		if got := report.Results[0].Facts.ObservedCount; got == nil || *got != 2 {
			t.Fatalf("event row count = %v, want 2", got)
		}
		if got := report.Results[1].Facts.ObservedFloat; got == nil || *got != 2.0 {
			t.Fatalf("event score average = %v, want 2.0", got)
		}
		perRow := report.Results[2]
		if perRow.Total != 2 || perRow.FailedCount != 1 || perRow.FailedPercent != 50 ||
			perRow.RowDenominator != gxsql.RowDenominatorAvailable {
			t.Fatalf("event per-row result = %#v, want total=2 failed=1 percent=50", perRow)
		}
		exported, err := gxsql.ExportReport(report, gxsql.IncludeCapturedDiagnostics())
		if err != nil {
			t.Fatalf("ExportReport: %v", err)
		}
		for i, result := range exported.Results {
			if result.Diagnostics == nil {
				t.Fatalf("event diagnostic %d = %#v, want scoped query diagnostics", i, result.Diagnostics)
			}
			query := strings.ToLower(result.Diagnostics.Query)
			if !strings.Contains(query, "where") ||
				!strings.Contains(query, "event_at >=") ||
				!strings.Contains(query, "event_at <") {
				t.Fatalf("event diagnostic %d = %#v, want half-open event_at scope", i, result.Diagnostics)
			}
		}
	})

	t.Run("empty scoped population", func(t *testing.T) {
		scope := gxsql.TrustedScope("tenant-empty", "tenant_id = ?", "tenant-none")
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.RowCount().Equal(0),
			gxsql.RowCount().GreaterThan(0),
			gxsql.Column("name").DistinctCount().Equal(1),
			gxsql.Float("score").AverageBetween(0, 100),
			gxsql.String("name").NotEmpty(),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertScopedQueries(t, db, "tenant_id", false, "tenant-none")
		if report.ScopeID != "tenant-empty" || len(report.Results) != 5 {
			t.Fatalf("scope/results = %q/%d, want tenant-empty/5", report.ScopeID, len(report.Results))
		}
		if result := report.Results[0]; !result.Success || result.Err != nil ||
			result.Facts.ObservedCount == nil || *result.Facts.ObservedCount != 0 {
			t.Fatalf("empty equal row count = %#v, want successful observed count 0", result)
		}
		for i := 1; i <= 2; i++ {
			result := report.Results[i]
			if result.Success || result.Err != nil || result.Facts.ObservedCount == nil || *result.Facts.ObservedCount != 0 {
				t.Fatalf("empty failing result %d = %#v, want failed observed count 0", i, result)
			}
		}
		if result := report.Results[3]; !result.Success || result.Err != nil {
			t.Fatalf("empty aggregate result = %#v, want successful empty aggregate", result)
		}
		if result := report.Results[4]; !result.Success || result.Err != nil ||
			result.RowDenominator != gxsql.RowDenominatorAvailable ||
			result.Total != 0 || result.FailedCount != 0 || result.FailedPercent != 0 {
			t.Fatalf("empty per-row result = %#v, want available denominator and zero metrics", result)
		}
	})

	t.Run("invalid scope aborts before execution", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		scope := gxsql.TrustedScope("invalid", "batch_id = ? AND tenant_id = ?", int64(1))
		report, err := gxsql.NewSuite(
			gxsql.RowCount().Equal(2),
			gxsql.String("name").NotEmpty(),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope))
		if err == nil || !errors.Is(err, gxsql.ErrCategoryInvalidConfig) {
			t.Fatalf("invalid scope error = %v, want invalid-config", err)
		}
		if len(report.Results) != 0 || report.Target != nil || report.ScopeID != "" {
			t.Fatalf("invalid scope report = %#v, want zero report", report)
		}
		if len(db.queries) != 0 {
			t.Fatalf("invalid scope queries = %d, want 0", len(db.queries))
		}
	})

	t.Run("tolerance max-failed-count boundaries", func(t *testing.T) {
		cases := []struct {
			name          string
			bare          gxsql.Expectation
			tol           gxsql.Expectation
			wantFailed    int
			wantSuccess   bool
			wantTolerated bool
		}{
			{
				name:          "perRow/zero",
				bare:          gxsql.Column("id").NotNull(),
				tol:           gxsql.WithMaxFailedCount(0, gxsql.Column("id").NotNull()),
				wantFailed:    0,
				wantSuccess:   true,
				wantTolerated: false,
			},
			{
				name:          "perRow/below",
				bare:          gxsql.String("name").NotEmpty(),
				tol:           gxsql.WithMaxFailedCount(2, gxsql.String("name").NotEmpty()),
				wantFailed:    1,
				wantSuccess:   true,
				wantTolerated: true,
			},
			{
				name:          "perRow/exact",
				bare:          gxsql.Int("age").Between(0, 120),
				tol:           gxsql.WithMaxFailedCount(2, gxsql.Int("age").Between(0, 120)),
				wantFailed:    2,
				wantSuccess:   true,
				wantTolerated: true,
			},
			{
				name:          "perRow/above",
				bare:          gxsql.Int("age").Between(15, 25),
				tol:           gxsql.WithMaxFailedCount(2, gxsql.Int("age").Between(15, 25)),
				wantFailed:    3,
				wantSuccess:   false,
				wantTolerated: false,
			},
			{
				name:          "unique/zero",
				bare:          gxsql.Column("id").Unique(),
				tol:           gxsql.WithMaxFailedCount(0, gxsql.Column("id").Unique()),
				wantFailed:    0,
				wantSuccess:   true,
				wantTolerated: false,
			},
			{
				name:          "unique/below",
				bare:          gxsql.Column("name").Unique(),
				tol:           gxsql.WithMaxFailedCount(3, gxsql.Column("name").Unique()),
				wantFailed:    2,
				wantSuccess:   true,
				wantTolerated: true,
			},
			{
				name:          "unique/exact",
				bare:          gxsql.Column("name").Unique(),
				tol:           gxsql.WithMaxFailedCount(2, gxsql.Column("name").Unique()),
				wantFailed:    2,
				wantSuccess:   true,
				wantTolerated: true,
			},
			{
				name:          "unique/above",
				bare:          gxsql.Column("nullable").Unique(),
				tol:           gxsql.WithMaxFailedCount(2, gxsql.Column("nullable").Unique()),
				wantFailed:    3,
				wantSuccess:   false,
				wantTolerated: false,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				bareReport, err := gxsql.NewSuite(tc.bare).ValidateTable(
					context.Background(), cfg.DB, cfg.Table,
					gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"), gxsql.WithSampleCap(10))
				if err != nil {
					t.Fatalf("bare ValidateTable: %v", err)
				}
				tolReport, err := gxsql.NewSuite(tc.tol).ValidateTable(
					context.Background(), cfg.DB, cfg.Table,
					gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"), gxsql.WithSampleCap(10))
				if err != nil {
					t.Fatalf("tolerant ValidateTable: %v", err)
				}
				bareRes := bareReport.Results[0]
				tolRes := tolReport.Results[0]
				assertToleranceRawPreservation(t, bareRes, tolRes)
				if tolRes.FailedCount != tc.wantFailed || tolRes.Success != tc.wantSuccess || tolRes.Tolerated != tc.wantTolerated {
					t.Fatalf("FailedCount=%d Success=%v Tolerated=%v, want %d/%v/%v",
						tolRes.FailedCount, tolRes.Success, tolRes.Tolerated,
						tc.wantFailed, tc.wantSuccess, tc.wantTolerated)
				}
				if tolRes.Facts.ConfiguredMaxFailedCount == nil {
					t.Fatal("ConfiguredMaxFailedCount is nil")
				}
				if bareRes.FailedCount != tc.wantFailed {
					t.Fatalf("bare FailedCount=%d, want %d (fixture drift)", bareRes.FailedCount, tc.wantFailed)
				}
			})
		}
	})

	t.Run("tolerance max-failed-percent and severity", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.WithPolicy(
				gxsql.Int("age").Between(0, 120),
				gxsql.Policy{
					Severity:    gxsql.SeverityWarning,
					Description: "age quality",
					Tags:        []string{"z", "a"},
					Tolerance:   gxsql.MaxFailedPercent(50),
				},
			),
			gxsql.WithPolicy(
				gxsql.Int("age").Between(0, 120),
				gxsql.Policy{
					Severity:  gxsql.SeverityError,
					Tolerance: gxsql.MaxFailedPercent(49.999),
				},
			),
		).ValidateTable(
			context.Background(), cfg.DB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		warning, failure := report.Results[0], report.Results[1]
		if !warning.Success || !warning.Tolerated || warning.Severity != gxsql.SeverityWarning ||
			warning.Facts.ConfiguredMaxFailedPercent == nil ||
			*warning.Facts.ConfiguredMaxFailedPercent != 50 {
			t.Fatalf("warning result = %#v, want tolerated 50%% policy pass", warning)
		}
		if failure.Success || failure.Severity != gxsql.SeverityError ||
			failure.Facts.ConfiguredMaxFailedPercent == nil ||
			*failure.Facts.ConfiguredMaxFailedPercent != 49.999 {
			t.Fatalf("error result = %#v, want hard 49.999%% policy failure", failure)
		}
		if report.OK() || report.Err() == nil || len(report.Warnings()) != 1 {
			t.Fatalf("gating = OK:%v Err:%v warnings:%d", report.OK(), report.Err(), len(report.Warnings()))
		}
		dto, err := gxsql.ExportReport(report)
		if err != nil {
			t.Fatalf("ExportReport: %v", err)
		}
		if dto.Results[0].Severity != "warning" || dto.Results[0].Facts == nil ||
			dto.Results[0].Facts.ConfiguredMaxFailedPercent == nil {
			t.Fatalf("exported warning policy = %#v", dto.Results[0])
		}
	})

	t.Run("tolerance scoped population ignores out-of-scope failures", func(t *testing.T) {
		scope := gxsql.TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")
		db := &recordingDB{DB: cfg.DB}

		unscoped, err := gxsql.NewSuite(
			gxsql.Int("age").Between(0, 120),
			gxsql.Column("name").Unique(),
		).ValidateTable(context.Background(), cfg.DB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"), gxsql.WithSampleCap(10))
		if err != nil {
			t.Fatalf("unscoped ValidateTable: %v", err)
		}
		if unscoped.Results[0].FailedCount != 2 {
			t.Fatalf("unscoped age FailedCount = %d, want 2 (includes out-of-scope row)", unscoped.Results[0].FailedCount)
		}
		if unscoped.Results[1].FailedCount != 2 {
			t.Fatalf("unscoped unique FailedCount = %d, want 2 (includes out-of-scope row)", unscoped.Results[1].FailedCount)
		}

		scoped, err := gxsql.NewSuite(
			gxsql.WithMaxFailedCount(1, gxsql.Int("age").Between(0, 120)),
			gxsql.WithMaxFailedCount(0, gxsql.Column("name").Unique()),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope),
			gxsql.WithKey("id"), gxsql.WithSampleCap(10))
		if err != nil {
			t.Fatalf("scoped ValidateTable: %v", err)
		}
		assertScopedQueries(t, db, "tenant_id", false, "tenant-a")
		if scoped.ScopeID != "tenant-a" || len(scoped.Results) != 2 {
			t.Fatalf("scope/results = %q/%d, want tenant-a/2", scoped.ScopeID, len(scoped.Results))
		}

		age := scoped.Results[0]
		if age.Total != 2 || age.FailedCount != 1 || age.FailedPercent != 50 ||
			!age.Success || !age.Tolerated || age.RowDenominator != gxsql.RowDenominatorAvailable {
			t.Fatalf("scoped age = %#v, want total=2 failed=1 percent=50 tolerated pass", age)
		}
		if len(age.FailedKeys) != 1 || len(age.FailedKeys[0]) != 1 || age.FailedKeys[0][0] != int64(2) {
			t.Fatalf("scoped age FailedKeys = %#v, want only in-scope id 2", age.FailedKeys)
		}
		if len(age.SampleValues) != 1 || age.SampleValues[0] != nil {
			t.Fatalf("scoped age SampleValues = %#v, want only the in-scope NULL failure", age.SampleValues)
		}

		unique := scoped.Results[1]
		if unique.Total != 2 || unique.FailedCount != 0 || unique.FailedPercent != 0 ||
			!unique.Success || unique.Tolerated || unique.RowDenominator != gxsql.RowDenominatorAvailable {
			t.Fatalf("scoped unique = %#v, want total=2 failed=0 clean pass", unique)
		}
		if len(unique.FailedKeys) != 0 || len(unique.SampleValues) != 0 {
			t.Fatalf("scoped unique diagnostics = keys %#v samples %#v, want empty", unique.FailedKeys, unique.SampleValues)
		}

		// Out-of-scope failure volume must not change the scoped verdict: the same
		// max=1 age policy fails unscoped (2 failures) and passes scoped (1 failure).
		unscopedTol, err := gxsql.NewSuite(
			gxsql.WithMaxFailedCount(1, gxsql.Int("age").Between(0, 120)),
		).ValidateTable(context.Background(), cfg.DB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"))
		if err != nil {
			t.Fatalf("unscoped tolerant ValidateTable: %v", err)
		}
		if unscopedTol.Results[0].Success || unscopedTol.Results[0].Tolerated || unscopedTol.Results[0].FailedCount != 2 {
			t.Fatalf("unscoped tolerant age = %#v, want above-bound failure with FailedCount=2", unscopedTol.Results[0])
		}
		if age.Success == unscopedTol.Results[0].Success {
			t.Fatal("scoped and unscoped verdicts must differ when out-of-scope failures tip the bound")
		}
	})

	t.Run("tolerance empty scope is not tolerated", func(t *testing.T) {
		scope := gxsql.TrustedScope("tenant-empty", "tenant_id = ?", "tenant-none")
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.WithMaxFailedCount(0, gxsql.String("name").NotEmpty()),
			gxsql.WithMaxFailedCount(2, gxsql.Column("name").Unique()),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope), gxsql.WithKey("id"))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertScopedQueries(t, db, "tenant_id", false, "tenant-none")
		if report.ScopeID != "tenant-empty" || len(report.Results) != 2 {
			t.Fatalf("scope/results = %q/%d, want tenant-empty/2", report.ScopeID, len(report.Results))
		}
		for i, res := range report.Results {
			if !res.Success || res.Tolerated || res.Err != nil ||
				res.RowDenominator != gxsql.RowDenominatorAvailable ||
				res.Total != 0 || res.FailedCount != 0 || res.FailedPercent != 0 ||
				math.IsNaN(res.FailedPercent) {
				t.Fatalf("empty-scope result %d = %#v, want clean zero pass without NaN", i, res)
			}
			if res.Facts.ConfiguredMaxFailedCount == nil {
				t.Fatalf("empty-scope result %d missing ConfiguredMaxFailedCount", i)
			}
		}
	})

	t.Run("tolerance adds no statements and reuses scoped total", func(t *testing.T) {
		bareDB := &recordingDB{DB: cfg.DB}
		if _, err := gxsql.NewSuite(
			gxsql.Int("age").Between(0, 120),
			gxsql.Column("name").Unique(),
		).ValidateTable(context.Background(), bareDB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id")); err != nil {
			t.Fatalf("bare ValidateTable: %v", err)
		}

		tolDB := &recordingDB{DB: cfg.DB}
		tolReport, err := gxsql.NewSuite(
			gxsql.WithMaxFailedCount(2, gxsql.Int("age").Between(0, 120)),
			gxsql.WithMaxFailedCount(2, gxsql.Column("name").Unique()),
		).ValidateTable(context.Background(), tolDB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"))
		if err != nil {
			t.Fatalf("tolerant ValidateTable: %v", err)
		}
		if len(tolDB.queries) != len(bareDB.queries) {
			t.Fatalf("tolerant queries = %d, bare = %d; wrapper must not add statements",
				len(tolDB.queries), len(bareDB.queries))
		}
		if !tolReport.Results[0].Success || !tolReport.Results[0].Tolerated || tolReport.Results[0].FailedCount != 2 {
			t.Fatalf("tolerant age = %#v, want exact-bound tolerated pass", tolReport.Results[0])
		}
		if !tolReport.Results[1].Success || !tolReport.Results[1].Tolerated || tolReport.Results[1].FailedCount != 2 {
			t.Fatalf("tolerant unique = %#v, want exact-bound tolerated pass", tolReport.Results[1])
		}

		scope := gxsql.TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")
		scopedDB := &recordingDB{DB: cfg.DB}
		scopedReport, err := gxsql.NewSuite(
			gxsql.WithMaxFailedCount(1, gxsql.Int("age").Between(0, 120)),
			gxsql.WithMaxFailedCount(0, gxsql.String("name").NotEmpty()),
			gxsql.WithMaxFailedCount(0, gxsql.Column("name").Unique()),
		).ValidateTable(context.Background(), scopedDB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope), gxsql.WithKey("id"))
		if err != nil {
			t.Fatalf("scoped ValidateTable: %v", err)
		}
		totals := scopedDenominatorTotalQueries(scopedDB.queries)
		if len(totals) != 1 {
			t.Fatalf("scoped denominator totals = %d, want exactly 1 shared total", len(totals))
		}
		assertScopedQueries(t, &recordingDB{DB: cfg.DB, queries: totals}, "tenant_id", false, "tenant-a")
		for i, res := range scopedReport.Results {
			if res.Total != 2 || res.RowDenominator != gxsql.RowDenominatorAvailable {
				t.Fatalf("scoped result %d = %#v, want shared total 2", i, res)
			}
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

	runRelationalKeyIntegrity(t, cfg)
	runCrossColumnRowInvariants(t, cfg)
	runTemporalAndFreshness(t, cfg)
	runStructuralColumnContracts(t, cfg)
	runPatternChecks(t, cfg)
}

func assertToleranceRawPreservation(t *testing.T, bare, tol gxsql.Result) {
	t.Helper()
	if tol.Total != bare.Total || tol.FailedCount != bare.FailedCount || tol.FailedPercent != bare.FailedPercent {
		t.Fatalf("raw counts changed: bare total/failed/percent=%d/%d/%v tol=%d/%d/%v",
			bare.Total, bare.FailedCount, bare.FailedPercent,
			tol.Total, tol.FailedCount, tol.FailedPercent)
	}
	if !reflect.DeepEqual(tol.SampleValues, bare.SampleValues) {
		t.Fatalf("SampleValues changed: bare=%#v tol=%#v", bare.SampleValues, tol.SampleValues)
	}
	if !reflect.DeepEqual(tol.FailedKeys, bare.FailedKeys) {
		t.Fatalf("FailedKeys changed: bare=%#v tol=%#v", bare.FailedKeys, tol.FailedKeys)
	}
	if tol.Kind != bare.Kind || tol.Name != bare.Name || tol.Column != bare.Column {
		t.Fatalf("identity changed: bare kind/name/col=%q/%q/%q tol=%q/%q/%q",
			bare.Kind, bare.Name, bare.Column, tol.Kind, tol.Name, tol.Column)
	}
	if tol.RowDenominator != bare.RowDenominator {
		t.Fatalf("RowDenominator changed: bare=%q tol=%q", bare.RowDenominator, tol.RowDenominator)
	}
}

func scopedDenominatorTotalQueries(queries []recordedQuery) []recordedQuery {
	var out []recordedQuery
	for _, q := range queries {
		upper := strings.ToUpper(q.text)
		if !strings.Contains(upper, "SELECT COUNT(*)") {
			continue
		}
		if strings.Contains(q.text, ") AND (") {
			continue
		}
		out = append(out, q)
	}
	return out
}
