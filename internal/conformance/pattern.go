package conformance

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/busyminds/gxsql"
)

func dialectAdvertisesRegex(d gxsql.Dialect) bool {
	_, ok := d.(gxsql.RegexDialect)
	return ok
}

func assertBoundArgsPresent(t *testing.T, db *recordingDB) {
	t.Helper()
	if len(db.queries) == 0 {
		t.Fatal("expected SQL queries with bound pattern args")
	}
	found := false
	for i, q := range db.queries {
		upper := strings.ToUpper(q.text)
		isPattern := strings.Contains(upper, "LIKE") ||
			strings.Contains(upper, "REGEXP") ||
			strings.Contains(q.text, " ~ ")
		if !isPattern {
			continue
		}
		found = true
		if len(q.args) == 0 {
			t.Fatalf("pattern query %d = %q has no bound args", i, q.text)
		}
		for _, arg := range q.args {
			if s, ok := arg.(string); ok && strings.Contains(q.text, "'"+s+"'") {
				t.Fatalf("pattern query %d embeds literal %q: %q", i, s, q.text)
			}
		}
	}
	if !found {
		t.Fatal("expected at least one LIKE/regex query with bound args")
	}
}

func assertNoSQL(t *testing.T, db *recordingDB) {
	t.Helper()
	if len(db.queries) != 0 {
		t.Fatalf("expected no SQL, got %d queries starting with %q", len(db.queries), db.queries[0].text)
	}
}

func assertUnsupportedRegex(t *testing.T, err error, dialect gxsql.Dialect) {
	t.Helper()
	if err == nil {
		t.Fatal("expected unsupported regex preflight error")
	}
	if !errors.Is(err, gxsql.ErrCategoryUnsupported) {
		t.Fatalf("error category = %v, want unsupported", err)
	}
	var preflight *gxsql.PreflightErrors
	if !errors.As(err, &preflight) || len(preflight.Issues) == 0 {
		t.Fatalf("error = %v, want PreflightErrors", err)
	}
	var unsupported *gxsql.UnsupportedCapabilityError
	if !errors.As(preflight.Issues[0].Err, &unsupported) {
		t.Fatalf("issue = %v, want UnsupportedCapabilityError", preflight.Issues[0].Err)
	}
	if unsupported.Kind != gxsql.KindRegex || unsupported.Capability != "regex" {
		t.Fatalf("unsupported = %#v, want kind=regex capability=regex", unsupported)
	}
	if unsupported.Dialect == "" {
		t.Fatal("unsupported capability error omitted dialect label")
	}
	if _, ok := dialect.(gxsql.RegexDialect); ok {
		t.Fatalf("dialect unexpectedly advertises RegexDialect while reporting unsupported: %#v", unsupported)
	}
}

func assertPatternFailure(t *testing.T, res gxsql.Result, kind gxsql.ExpectationKind, total, failed int) {
	t.Helper()
	if res.Kind != kind {
		t.Fatalf("Kind = %q, want %q", res.Kind, kind)
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if res.Total != total || res.FailedCount != failed {
		t.Fatalf("Total/FailedCount = %d/%d, want %d/%d", res.Total, res.FailedCount, total, failed)
	}
}

// runPatternChecks exercises portable LIKE-family builders and capability-gated
// Regex against the shared users fixture (names: alice, "", alice, zed).
func runPatternChecks(t *testing.T, cfg Config) {
	t.Helper()
	table := cfg.Table

	t.Run("pattern/like-family on users.name", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.String("name").HasPrefix("ali"),
			gxsql.String("name").HasSuffix("ed"),
			gxsql.String("name").Contains("li"),
			gxsql.String("name").Like("a%"),
			gxsql.String("name").NotLike("zed"),
			gxsql.String("nullable").HasPrefix("present"), // NULL row fails
		).ValidateTable(context.Background(), db, table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"), gxsql.WithFailedKeysCap(0))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertBoundArgsPresent(t, db)
		assertPatternFailure(t, report.Results[0], gxsql.KindHasPrefix, 4, 2) // alice, alice
		assertPatternFailure(t, report.Results[1], gxsql.KindHasSuffix, 4, 3) // zed
		assertPatternFailure(t, report.Results[2], gxsql.KindContains, 4, 2)  // alice, alice
		assertPatternFailure(t, report.Results[3], gxsql.KindLike, 4, 2)      // alice, alice
		assertPatternFailure(t, report.Results[4], gxsql.KindNotLike, 4, 1)   // zed
		assertPatternFailure(t, report.Results[5], gxsql.KindHasPrefix, 4, 1) // NULL nullable
	})

	t.Run("pattern/literal escape and raw Like", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.String("name").HasPrefix("ali%"),
			gxsql.String("name").Contains("_"),
			gxsql.String("name").Like("a%"),
		).ValidateTable(context.Background(), db, table, gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertBoundArgsPresent(t, db)
		assertPatternFailure(t, report.Results[0], gxsql.KindHasPrefix, 4, 4)
		assertPatternFailure(t, report.Results[1], gxsql.KindContains, 4, 4)
		assertPatternFailure(t, report.Results[2], gxsql.KindLike, 4, 2)

		sawEscaped, sawRaw := false, false
		for _, q := range db.queries {
			upper := strings.ToUpper(q.text)
			if !strings.Contains(upper, "LIKE") {
				continue
			}
			if strings.Contains(upper, "ESCAPE") {
				sawEscaped = true
			} else {
				sawRaw = true
			}
		}
		if !sawEscaped {
			t.Fatal("expected ESCAPE on literal-fragment LIKE")
		}
		if !sawRaw {
			t.Fatal("expected raw Like without ESCAPE")
		}
	})

	t.Run("pattern/scope empty population", func(t *testing.T) {
		scope := gxsql.TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.String("name").HasPrefix("ali"),
		).ValidateTable(context.Background(), db, table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertScopedQueries(t, db, "tenant_id", false, "tenant-a")
		assertPatternFailure(t, report.Results[0], gxsql.KindHasPrefix, 2, 1) // alice + ""

		emptyScope := gxsql.TrustedScope("empty-pattern", "tenant_id = ?", "missing-tenant")
		emptyReport, err := gxsql.NewSuite(
			gxsql.String("name").Contains("ali"),
			gxsql.String("name").Like("%"),
			gxsql.String("name").NotLike("%"),
		).ValidateTable(context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(emptyScope))
		if err != nil {
			t.Fatalf("empty scoped ValidateTable: %v", err)
		}
		for i, res := range emptyReport.Results {
			if !res.Success || res.Total != 0 || res.FailedCount != 0 || res.Err != nil {
				t.Fatalf("empty scoped result %d = %#v, want vacuous success", i, res)
			}
		}
	})

	if dialectAdvertisesRegex(cfg.Dialect) {
		t.Run("pattern/regex supported", func(t *testing.T) {
			db := &recordingDB{DB: cfg.DB}
			report, err := gxsql.NewSuite(
				gxsql.String("name").Regex(`^alice$`),
			).ValidateTable(context.Background(), db, table,
				gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"), gxsql.WithFailedKeysCap(0))
			if err != nil {
				t.Fatalf("ValidateTable: %v", err)
			}
			assertBoundArgsPresent(t, db)
			assertPatternFailure(t, report.Results[0], gxsql.KindRegex, 4, 2)
			for _, q := range db.queries {
				if strings.Contains(strings.ToUpper(q.text), "LIKE") {
					t.Fatalf("regex must not rewrite to LIKE: %q", q.text)
				}
			}
		})
	} else {
		t.Run("pattern/regex unsupported preflight without SQL", func(t *testing.T) {
			db := &recordingDB{DB: cfg.DB}
			_, err := gxsql.NewSuite(
				gxsql.String("name").Regex(`^alice$`),
			).ValidateTable(context.Background(), db, table, gxsql.WithDialect(cfg.Dialect))
			assertUnsupportedRegex(t, err, cfg.Dialect)
			assertNoSQL(t, db)

			contDB := &recordingDB{DB: cfg.DB}
			report, err := gxsql.NewSuite(
				gxsql.String("name").Regex(`^alice$`),
				gxsql.String("name").HasPrefix("ali"),
			).ValidateTable(context.Background(), contDB, table,
				gxsql.WithDialect(cfg.Dialect), gxsql.ContinueOnError())
			if err != nil {
				t.Fatalf("ContinueOnError: %v", err)
			}
			if report.Results[0].Err == nil || report.Results[0].Success {
				t.Fatalf("regex slot = %#v, want unsupported Result.Err", report.Results[0])
			}
			assertPatternFailure(t, report.Results[1], gxsql.KindHasPrefix, 4, 2)
			for _, q := range contDB.queries {
				if strings.Contains(strings.ToUpper(q.text), "REGEXP") ||
					strings.Contains(q.text, " ~ ") {
					t.Fatalf("unsupported regex must not emit regex SQL: %q", q.text)
				}
			}
		})
	}
}
