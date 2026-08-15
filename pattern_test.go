package gxsql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPatternPredicateRenderingAndBinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		build    func() Expectation
		check    func(t *testing.T, d Dialect, pred rowPredicate)
		dialects []Dialect
	}{
		{
			name:     "HasPrefix escapes literal wildcards",
			build:    func() Expectation { return String("code").HasPrefix(`ACME_%\`) },
			dialects: []Dialect{Postgres(), SQLite(), DuckDB(), MySQL()},
			check: func(t *testing.T, d Dialect, pred rowPredicate) {
				t.Helper()
				for _, sub := range []string{"IS NULL", "NOT LIKE", "ESCAPE"} {
					if !strings.Contains(pred.where, sub) {
						t.Fatalf("%T where=%q missing %q", d, pred.where, sub)
					}
				}
				if !strings.Contains(pred.where, likeEscapeClause(d)) {
					t.Fatalf("%T where=%q missing dialect ESCAPE clause %q", d, pred.where, likeEscapeClause(d))
				}
				wantArg := escapeLikeLiteral(`ACME_%\`) + "%"
				if len(pred.args) != 1 || pred.args[0] != wantArg {
					t.Fatalf("%T args=%#v, want [%q]", d, pred.args, wantArg)
				}
				assertDialectPlaceholder(t, d, pred.where)
			},
		},
		{
			name:     "HasSuffix escapes and wraps",
			build:    func() Expectation { return String("path").HasSuffix("_end%") },
			dialects: []Dialect{Postgres(), SQLite(), DuckDB(), MySQL()},
			check: func(t *testing.T, d Dialect, pred rowPredicate) {
				t.Helper()
				wantArg := "%" + escapeLikeLiteral("_end%")
				if len(pred.args) != 1 || pred.args[0] != wantArg {
					t.Fatalf("%T args=%#v, want [%q]", d, pred.args, wantArg)
				}
				if !strings.Contains(pred.where, "NOT LIKE") || !strings.Contains(pred.where, "ESCAPE") {
					t.Fatalf("%T where=%q", d, pred.where)
				}
			},
		},
		{
			name:     "Contains escapes and wraps both sides",
			build:    func() Expectation { return String("path").Contains(`/in_box/%`) },
			dialects: []Dialect{Postgres(), SQLite(), DuckDB(), MySQL()},
			check: func(t *testing.T, d Dialect, pred rowPredicate) {
				t.Helper()
				wantArg := "%" + escapeLikeLiteral(`/in_box/%`) + "%"
				if len(pred.args) != 1 || pred.args[0] != wantArg {
					t.Fatalf("%T args=%#v, want [%q]", d, pred.args, wantArg)
				}
			},
		},
		{
			name:     "Like preserves caller wildcards without ESCAPE",
			build:    func() Expectation { return String("email").Like("%@ex.com") },
			dialects: []Dialect{Postgres(), SQLite(), DuckDB(), MySQL()},
			check: func(t *testing.T, d Dialect, pred rowPredicate) {
				t.Helper()
				if strings.Contains(pred.where, "ESCAPE") {
					t.Fatalf("%T Like must not add ESCAPE: %q", d, pred.where)
				}
				if !strings.Contains(pred.where, "NOT LIKE") {
					t.Fatalf("%T where=%q", d, pred.where)
				}
				if len(pred.args) != 1 || pred.args[0] != "%@ex.com" {
					t.Fatalf("%T args=%#v, want raw pattern", d, pred.args)
				}
			},
		},
		{
			name:     "NotLike preserves caller wildcards and inverts operator",
			build:    func() Expectation { return String("sku").NotLike("%-TMP") },
			dialects: []Dialect{Postgres(), SQLite(), DuckDB(), MySQL()},
			check: func(t *testing.T, d Dialect, pred rowPredicate) {
				t.Helper()
				if strings.Contains(pred.where, "ESCAPE") {
					t.Fatalf("%T NotLike must not add ESCAPE: %q", d, pred.where)
				}
				if !strings.Contains(pred.where, " LIKE ") || strings.Contains(pred.where, "NOT LIKE") {
					t.Fatalf("%T where=%q, want bare LIKE failure predicate", d, pred.where)
				}
				if len(pred.args) != 1 || pred.args[0] != "%-TMP" {
					t.Fatalf("%T args=%#v", d, pred.args)
				}
			},
		},
		{
			name:     "Regex supported dialects",
			build:    func() Expectation { return String("ref").Regex(`^[A-Z]+$`) },
			dialects: []Dialect{Postgres(), DuckDB(), MySQL()},
			check: func(t *testing.T, d Dialect, pred rowPredicate) {
				t.Helper()
				if strings.Contains(strings.ToUpper(pred.where), "LIKE") {
					t.Fatalf("%T regex must not rewrite to LIKE: %q", d, pred.where)
				}
				if !strings.Contains(pred.where, "IS NULL") || !strings.Contains(pred.where, "NOT (") {
					t.Fatalf("%T where=%q", d, pred.where)
				}
				switch d.(type) {
				case mysqlDialect:
					if !strings.Contains(pred.where, " REGEXP ") {
						t.Fatalf("mysql where=%q", pred.where)
					}
				default:
					if !strings.Contains(pred.where, " ~ ") {
						t.Fatalf("%T where=%q", d, pred.where)
					}
				}
				if len(pred.args) != 1 || pred.args[0] != `^[A-Z]+$` {
					t.Fatalf("%T args=%#v", d, pred.args)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			exp := tc.build().(perRowExpectation)
			for _, d := range tc.dialects {
				d := d
				pred, err := exp.build(d, exp.column, nil)
				if err != nil {
					t.Fatalf("%T build: %v", d, err)
				}
				tc.check(t, d, pred)
			}
		})
	}
}

func assertDialectPlaceholder(t *testing.T, d Dialect, where string) {
	t.Helper()
	switch d.(type) {
	case postgresDialect, duckdbDialect:
		if !strings.Contains(where, "$1") {
			t.Fatalf("%T where=%q, want $1", d, where)
		}
	case sqliteDialect, mysqlDialect:
		if !strings.Contains(where, "?") {
			t.Fatalf("%T where=%q, want ?", d, where)
		}
		if strings.Contains(where, "$") {
			t.Fatalf("%T where=%q must not use $n", d, where)
		}
	}
}

func assertPatternResult(t *testing.T, res Result, kind ExpectationKind, column string, total, failed int, keys []RowKey) {
	t.Helper()
	if res.Kind != kind {
		t.Fatalf("Kind=%q want %q", res.Kind, kind)
	}
	if res.Column != column {
		t.Fatalf("Column=%q want %q", res.Column, column)
	}
	if res.Total != total {
		t.Fatalf("Total=%d want %d", res.Total, total)
	}
	if res.FailedCount != failed {
		t.Fatalf("FailedCount=%d want %d (result=%#v)", res.FailedCount, failed, res)
	}
	if keys != nil && !reflect.DeepEqual(res.FailedKeys, keys) {
		t.Fatalf("FailedKeys=%#v want %#v", res.FailedKeys, keys)
	}
}

func TestPatternLikeFamilyCountsAndKeys(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "code": "ACME_1", "email": "a@ex.com", "sku": "AB-OK", "path": "x/in_box/%/y"},
		map[string]any{"id": int64(2), "code": "ACME%1", "email": "b@other.com", "sku": "AB-TMP", "path": "nope"},
		map[string]any{"id": int64(3), "code": "OTHER", "email": "c@ex.com", "sku": "ZZ-OK", "path": "in_box"},
		map[string]any{"id": int64(4), "code": nil, "email": nil, "sku": nil, "path": nil},
	))

	for _, d := range []struct {
		name string
		d    Dialect
	}{
		{"postgres", Postgres()},
		{"sqlite", SQLite()},
		{"duckdb", DuckDB()},
		{"mysql", MySQL()},
	} {
		tc := d
		t.Run(tc.name, func(t *testing.T) {
			db := openHarnessDB(t)
			rep, err := NewSuite(
				String("code").HasPrefix("ACME_"),
				String("code").HasSuffix("%1"),
				String("path").Contains("in_box/%"),
				String("email").Like("%@ex.com"),
				String("sku").NotLike("%-TMP"),
			).ValidateTable(context.Background(), db, Table("users"), WithDialect(tc.d), WithKey("id"))
			if err != nil {
				t.Fatalf("ValidateTable: %v", err)
			}

			assertPatternResult(t, rep.Results[0], KindHasPrefix, "code", 4, 3, []RowKey{{int64(2)}, {int64(3)}, {int64(4)}})
			assertPatternResult(t, rep.Results[1], KindHasSuffix, "code", 4, 3, []RowKey{{int64(1)}, {int64(3)}, {int64(4)}})
			assertPatternResult(t, rep.Results[2], KindContains, "path", 4, 3, []RowKey{{int64(2)}, {int64(3)}, {int64(4)}})
			assertPatternResult(t, rep.Results[3], KindLike, "email", 4, 2, []RowKey{{int64(2)}, {int64(4)}})
			assertPatternResult(t, rep.Results[4], KindNotLike, "sku", 4, 2, []RowKey{{int64(2)}, {int64(4)}})
		})
	}
}

func TestPatternEmptyScopedPopulationPasses(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "t2", "code": "NOPE", "email": "x"},
	))
	db := openHarnessDB(t)
	rep, err := NewSuite(
		String("code").HasPrefix("ACME"),
		String("email").Like("%@ex.com"),
		String("code").Regex(`^ACME`),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("empty", "tenant_id = ?", "t1")),
	)
	if err != nil {
		t.Fatal(err)
	}
	for i, res := range rep.Results {
		if !res.Success || res.Total != 0 || res.FailedCount != 0 {
			t.Fatalf("result[%d]=%#v, want vacuous pass", i, res)
		}
	}
}

func TestPatternWithScopeBindOrder(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "t1", "code": "ACME_1"},
		map[string]any{"id": int64(2), "tenant_id": "t1", "code": "OTHER"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "code": "ACME_9"},
	))
	db := openRecordingHarnessDB(t)
	scope := TrustedScope("tenant", "tenant_id = ?", "t1")
	rep, err := NewSuite(String("code").HasPrefix("ACME_")).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithScope(scope), WithKey("id"), CaptureQueryDiagnostics(),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Total != 2 || res.FailedCount != 1 {
		t.Fatalf("counts total=%d failed=%d", res.Total, res.FailedCount)
	}
	if !reflect.DeepEqual(res.FailedKeys, []RowKey{{int64(2)}}) {
		t.Fatalf("FailedKeys=%#v", res.FailedKeys)
	}
	if len(db.queries) < 2 {
		t.Fatalf("queries=%d", len(db.queries))
	}
	failure := db.queries[1]
	if !strings.Contains(failure.text, "tenant_id = $1") {
		t.Fatalf("scoped failure missing scope placeholder: %q", failure.text)
	}
	if !strings.Contains(failure.text, "NOT LIKE $2") {
		t.Fatalf("scoped failure missing expectation placeholder $2: %q", failure.text)
	}
	if len(failure.args) < 2 || failure.args[0] != "t1" || failure.args[1] != escapeLikeLiteral("ACME_")+"%" {
		t.Fatalf("bind order args=%#v", failure.args)
	}
}

func TestPatternWithKeySampleFailureCapsToleranceAndDeclarationOrder(t *testing.T) {
	rows := []map[string]any{
		{"id": int64(1), "code": "NO", "email": "a@ex.com"},
		{"id": int64(2), "code": "NO", "email": "b@ex.com"},
		{"id": int64(3), "code": "NO", "email": "c@ex.com"},
		{"id": int64(4), "code": "ACME", "email": "d@ex.com"},
	}
	setHarnessData(t, harnessUsers(rows...))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		WithID("prefix", WithMaxFailedCount(3, String("code").HasPrefix("ACME"))),
		WithID("like", String("email").Like("%@ex.com")),
		WithID("contains", String("code").Contains("X")),
	).WithSampleCap(1).WithFailedKeysCap(1).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results=%d", len(rep.Results))
	}
	if rep.Results[0].ID != "prefix" || rep.Results[1].ID != "like" || rep.Results[2].ID != "contains" {
		t.Fatalf("declaration order IDs=%q,%q,%q", rep.Results[0].ID, rep.Results[1].ID, rep.Results[2].ID)
	}
	if rep.Results[0].Kind != KindHasPrefix || rep.Results[1].Kind != KindLike || rep.Results[2].Kind != KindContains {
		t.Fatalf("kinds=%q,%q,%q", rep.Results[0].Kind, rep.Results[1].Kind, rep.Results[2].Kind)
	}
	if rep.Results[0].Column != "code" || rep.Results[1].Column != "email" {
		t.Fatalf("columns=%q,%q", rep.Results[0].Column, rep.Results[1].Column)
	}

	tol := rep.Results[0]
	if !tol.Success || !tol.Tolerated || tol.FailedCount != 3 || tol.Total != 4 {
		t.Fatalf("tolerated prefix=%#v", tol)
	}
	if len(tol.SampleValues) != 1 || len(tol.FailedKeys) != 1 {
		t.Fatalf("caps samples=%#v keys=%#v", tol.SampleValues, tol.FailedKeys)
	}
	if !rep.Results[1].Success || rep.Results[1].FailedCount != 0 {
		t.Fatalf("like=%#v", rep.Results[1])
	}
	if rep.Results[2].Success || rep.Results[2].FailedCount != 4 {
		t.Fatalf("contains=%#v", rep.Results[2])
	}
	if len(rep.Results[2].SampleValues) != 1 || len(rep.Results[2].FailedKeys) != 1 {
		t.Fatalf("contains caps samples=%#v keys=%#v", rep.Results[2].SampleValues, rep.Results[2].FailedKeys)
	}

	summary, err := NewSuite(String("code").HasPrefix("ACME")).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"), SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Results[0].FailedCount != 3 {
		t.Fatalf("SummaryOnly FailedCount=%d, want complete count 3", summary.Results[0].FailedCount)
	}
	if len(summary.Results[0].FailedKeys) != 0 || len(summary.Results[0].SampleValues) == 0 {
		t.Fatalf("SummaryOnly must suppress keys and retain samples: %#v", summary.Results[0])
	}
}

func TestPatternExportDefaultSuppressionAndCapturedDiagnostics(t *testing.T) {
	secret := "SECRET_PREFIX_VALUE"
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "code": "nope"},
	))
	db := openHarnessDB(t)
	rep, err := NewSuite(String("code").HasPrefix(secret)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), CaptureQueryDiagnostics(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].diagnostics == nil || !strings.Contains(rep.Results[0].diagnostics.query, "NOT LIKE") {
		t.Fatalf("expected captured diagnostics, got %#v", rep.Results[0].diagnostics)
	}
	if rep.Results[0].Facts.ConfiguredBound != nil {
		t.Fatalf("pattern facts must omit ConfiguredBound, got %#v", rep.Results[0].Facts.ConfiguredBound)
	}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Results[0].DisplayName != "code has prefix (...)" {
		t.Fatalf("DisplayName=%q", dto.Results[0].DisplayName)
	}
	if strings.Contains(dto.Results[0].DisplayName, secret) {
		t.Fatalf("display leaked secret: %q", dto.Results[0].DisplayName)
	}
	if dto.Results[0].Diagnostics != nil {
		t.Fatalf("default export included diagnostics: %#v", dto.Results[0].Diagnostics)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatalf("default export leaked pattern literal: %s", raw)
	}
	if bytes.Contains(raw, []byte("configured_bound")) {
		t.Fatalf("default export leaked configured_bound: %s", raw)
	}
	if bytes.Contains(raw, []byte("NOT LIKE")) || bytes.Contains(raw, []byte("SELECT")) {
		t.Fatalf("default export leaked SQL: %s", raw)
	}

	optIn, err := ExportReport(rep, IncludeCapturedDiagnostics(), IncludeCapturedArguments())
	if err != nil {
		t.Fatal(err)
	}
	if optIn.Results[0].Diagnostics == nil || optIn.Results[0].Diagnostics.Query == "" {
		t.Fatalf("opt-in diagnostics missing: %#v", optIn.Results[0].Diagnostics)
	}
	if !strings.Contains(optIn.Results[0].Diagnostics.Query, "NOT LIKE") {
		t.Fatalf("opt-in query=%q", optIn.Results[0].Diagnostics.Query)
	}
}

func TestPatternRegexPositiveNegativeAndNULL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ref": "ABC"},
		map[string]any{"id": int64(2), "ref": "abc"},
		map[string]any{"id": int64(3), "ref": nil},
	))
	for _, d := range []Dialect{Postgres(), DuckDB(), MySQL()} {
		d := d
		t.Run(dialectLabel(d), func(t *testing.T) {
			db := openHarnessDB(t)
			rep, err := NewSuite(String("ref").Regex(`^[A-Z]+$`)).ValidateTable(
				context.Background(), db, Table("users"),
				WithDialect(d), WithKey("id"),
			)
			if err != nil {
				t.Fatal(err)
			}
			assertPatternResult(t, rep.Results[0], KindRegex, "ref", 3, 2, []RowKey{{int64(2)}, {int64(3)}})
			if rep.Results[0].diagnostics != nil && strings.Contains(strings.ToUpper(rep.Results[0].diagnostics.query), "LIKE") {
				t.Fatalf("regex must not use LIKE SQL: %#v", rep.Results[0].diagnostics)
			}
		})
	}
}

func TestPatternRegexUnsupportedSQLitePreflightNoSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ref": "ABC"},
	))
	counter := openCountingHarnessDB(t)
	_, err := NewSuite(String("ref").Regex(`^[A-Z]+$`)).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(SQLite()),
	)
	if err == nil {
		t.Fatal("expected unsupported regex preflight error")
	}
	if !errors.Is(err, ErrCategoryUnsupported) {
		t.Fatalf("category=%v", err)
	}
	var uc *UnsupportedCapabilityError
	if !errors.As(err, &uc) {
		t.Fatalf("want UnsupportedCapabilityError, got %T %v", err, err)
	}
	if uc.Kind != KindRegex || uc.Dialect != "sqlite" || uc.Capability != "regex" {
		t.Fatalf("UnsupportedCapabilityError=%#v", uc)
	}
	if counter.queries != 0 {
		t.Fatalf("queries=%d, want 0", counter.queries)
	}
	var pe *PreflightErrors
	if !errors.As(err, &pe) {
		t.Fatalf("want PreflightErrors, got %v", err)
	}
}

func TestPatternRegexUnsupportedContinueOnErrorNoSQLNoFallback(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ref": "ABC", "email": "a@ex.com"},
	))
	db := openRecordingHarnessDB(t)
	rep, err := NewSuite(
		WithID("regex", String("ref").Regex(`^[A-Z]+$`)),
		WithID("like", String("email").Like("%@ex.com")),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(SQLite()), ContinueOnError(), CaptureQueryDiagnostics(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError top-level err=%v", err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results=%d", len(rep.Results))
	}
	if rep.Results[0].Success || !errors.Is(rep.Results[0].Err, ErrCategoryUnsupported) {
		t.Fatalf("regex slot=%#v", rep.Results[0])
	}
	var uc *UnsupportedCapabilityError
	if !errors.As(rep.Results[0].Err, &uc) || uc.Capability != "regex" {
		t.Fatalf("regex err=%v", rep.Results[0].Err)
	}
	if rep.Results[0].diagnostics != nil {
		t.Fatalf("unsupported regex must not capture SQL: %#v", rep.Results[0].diagnostics)
	}
	for _, q := range db.queries {
		if strings.Contains(strings.ToUpper(q.text), "LIKE") && strings.Contains(q.text, "ref") {
			t.Fatalf("regex must not fall back to LIKE: %#v", q)
		}
		if strings.Contains(q.text, "~") || strings.Contains(strings.ToUpper(q.text), "REGEXP") {
			t.Fatalf("unsupported regex issued regex SQL: %#v", q)
		}
	}
	if !rep.Results[1].Success || rep.Results[1].Kind != KindLike {
		t.Fatalf("later like should run: %#v", rep.Results[1])
	}
	if len(db.queries) == 0 {
		t.Fatal("valid Like expectation should execute SQL")
	}
}

type patternRegexCapabilityDialect struct {
	Dialect
	cap RegexCapability
}

func (d patternRegexCapabilityDialect) RegexCapability() RegexCapability {
	return d.cap
}

func completeSubstringRegexCapability() RegexCapability {
	return RegexCapability{
		Name:          regexCapabilityName,
		Operator:      "~",
		Flags:         "",
		Match:         RegexMatchSubstring,
		NullBehavior:  RegexNullsFail,
		UnicodeLimits: "engine-defined",
	}
}

func assertRegexCapabilityPreflightNoSQL(t *testing.T, d Dialect, capability string, counter *countingDB) {
	t.Helper()
	_, err := NewSuite(String("ref").Regex(`^[A-Z]+$`)).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(d),
	)
	if err == nil {
		t.Fatal("expected unsupported regex capability preflight error")
	}
	if !errors.Is(err, ErrCategoryUnsupported) {
		t.Fatalf("category=%v", err)
	}
	var uc *UnsupportedCapabilityError
	if !errors.As(err, &uc) {
		t.Fatalf("want UnsupportedCapabilityError, got %T %v", err, err)
	}
	if uc.Kind != KindRegex || uc.Capability != capability {
		t.Fatalf("UnsupportedCapabilityError=%#v, want capability %q", uc, capability)
	}
	if counter.queries != 0 {
		t.Fatalf("queries=%d, want 0", counter.queries)
	}
	var pe *PreflightErrors
	if !errors.As(err, &pe) {
		t.Fatalf("want PreflightErrors, got %v", err)
	}
}

func TestPatternRegexRejectsNonEmptyFlagsPreflightNoSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ref": "ABC"},
	))
	counter := openCountingHarnessDB(t)
	cap := completeSubstringRegexCapability()
	cap.Flags = "i"
	d := patternRegexCapabilityDialect{Dialect: Postgres(), cap: cap}
	assertRegexCapabilityPreflightNoSQL(t, d, "regex.flags", counter)
}

func TestPatternRegexRejectsFullMatchPreflightNoSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ref": "ABC"},
	))
	counter := openCountingHarnessDB(t)
	cap := completeSubstringRegexCapability()
	cap.Match = RegexMatchFull
	d := patternRegexCapabilityDialect{Dialect: Postgres(), cap: cap}
	assertRegexCapabilityPreflightNoSQL(t, d, "regex.match", counter)
}

func TestPatternStableKindsAndColumns(t *testing.T) {
	cases := []struct {
		exp  Expectation
		kind ExpectationKind
		col  string
	}{
		{String("code").HasPrefix("A"), KindHasPrefix, "code"},
		{String("code").HasSuffix("Z"), KindHasSuffix, "code"},
		{String("code").Contains("M"), KindContains, "code"},
		{String("code").Like("%M%"), KindLike, "code"},
		{String("code").NotLike("%X%"), KindNotLike, "code"},
		{String("code").Regex("^A"), KindRegex, "code"},
	}
	for _, tc := range cases {
		exp := tc.exp.(perRowExpectation)
		if exp.expectationKind() != tc.kind || string(tc.kind) == "" {
			t.Fatalf("kind=%q want %q", exp.expectationKind(), tc.kind)
		}
		if exp.column != tc.col {
			t.Fatalf("column=%q", exp.column)
		}
	}
}

func TestPatternExportRedactsPreflightFailureNames(t *testing.T) {
	secret := "PREFLIGHT_SECRET"
	db := openHarnessDB(t)
	report, err := NewSuite(String("").HasPrefix(secret)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := ExportReport(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(exported.Results[0].DisplayName, secret) {
		t.Fatalf("display name leaked pattern: %q", exported.Results[0].DisplayName)
	}
	if exported.Results[0].DisplayName != "pattern has prefix (...)" {
		t.Fatalf("DisplayName=%q", exported.Results[0].DisplayName)
	}
}
