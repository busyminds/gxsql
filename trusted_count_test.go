package gxsql

import (
	"errors"
	"testing"
)

func TestTrustedCountTemplateRenderUnscoped(t *testing.T) {
	t.Parallel()

	template := "SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND status = ?"
	sql, args, err := renderTrustedCount(Postgres(), Table("orders"), nil, template, []any{"active"})
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := `SELECT COUNT(*) FROM "orders" WHERE TRUE AND status = $1`
	if sql != wantSQL {
		t.Fatalf("sql = %q, want %q", sql, wantSQL)
	}
	if len(args) != 1 || args[0] != "active" {
		t.Fatalf("args = %v, want [active]", args)
	}
}

func TestTrustedCountTemplateRenderScoped(t *testing.T) {
	t.Parallel()

	scope, err := newTrustedScope("pop", "tenant_id = ? AND region = ?", []any{"t1", "us"})
	if err != nil {
		t.Fatal(err)
	}
	template := "SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND status = ?"

	tests := []struct {
		name    string
		dialect Dialect
		wantSQL string
	}{
		{
			name:    "postgres",
			dialect: Postgres(),
			wantSQL: `SELECT COUNT(*) FROM "orders" WHERE (tenant_id = $1 AND region = $2) AND status = $3`,
		},
		{
			name:    "sqlite",
			dialect: SQLite(),
			wantSQL: `SELECT COUNT(*) FROM "orders" WHERE (tenant_id = ? AND region = ?) AND status = ?`,
		},
		{
			name:    "mysql",
			dialect: MySQL(),
			wantSQL: "SELECT COUNT(*) FROM `orders` WHERE (tenant_id = ? AND region = ?) AND status = ?",
		},
		{
			name:    "duckdb",
			dialect: DuckDB(),
			wantSQL: `SELECT COUNT(*) FROM "orders" WHERE (tenant_id = $1 AND region = $2) AND status = $3`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, err := renderTrustedCount(tc.dialect, Table("orders"), &scope, template, []any{"active"})
			if err != nil {
				t.Fatal(err)
			}
			if sql != tc.wantSQL {
				t.Fatalf("sql = %q, want %q", sql, tc.wantSQL)
			}
			wantArgs := []any{"t1", "us", "active"}
			if len(args) != len(wantArgs) {
				t.Fatalf("args = %v, want %v", args, wantArgs)
			}
			for i := range wantArgs {
				if args[i] != wantArgs[i] {
					t.Fatalf("args[%d] = %v, want %v", i, args[i], wantArgs[i])
				}
			}
		})
	}
}

func TestTrustedCountMarkerPreflightRejectsMissingAndDuplicate(t *testing.T) {
	t.Parallel()

	valid := "SELECT COUNT(*) FROM {{target}} WHERE {{scope}}"
	cases := []struct {
		name     string
		template string
		wantErr  error
	}{
		{
			name:     "missing target",
			template: "SELECT COUNT(*) FROM orders WHERE {{scope}}",
			wantErr:  errTrustedCountTargetMarkerRequired,
		},
		{
			name:     "missing scope",
			template: "SELECT COUNT(*) FROM {{target}} WHERE TRUE",
			wantErr:  errTrustedCountScopeMarkerRequired,
		},
		{
			name:     "duplicate target",
			template: valid + " UNION ALL SELECT 1 FROM {{target}} WHERE {{scope}}",
			wantErr:  errTrustedCountDuplicateTargetMarker,
		},
		{
			name:     "duplicate scope",
			template: "SELECT COUNT(*) FROM {{target}} WHERE {{scope}} OR {{scope}}",
			wantErr:  errTrustedCountDuplicateScopeMarker,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := preflightTrustedCount(tc.template, nil)
			if err == nil {
				t.Fatal("expected preflight error")
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) {
				t.Fatalf("category = %v, want invalid_config", err)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestTrustedCountMarkerIgnoresQuotedAndCommentedMarkers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		template string
		wantErr  error
	}{
		{
			name:     "quoted target",
			template: "SELECT COUNT(*) FROM '{{target}}' WHERE {{scope}}",
			wantErr:  errTrustedCountTargetMarkerRequired,
		},
		{
			name:     "commented scope",
			template: "SELECT COUNT(*) FROM {{target}} WHERE TRUE -- {{scope}}",
			wantErr:  errTrustedCountScopeMarkerRequired,
		},
		{
			name:     "block comment markers",
			template: "SELECT COUNT(*) FROM /* {{target}} */ {{target}} WHERE /* {{scope}} */ {{scope}}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := preflightTrustedCount(tc.template, nil)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatal("expected preflight error")
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected preflight error: %v", err)
			}
		})
	}
}

func TestTrustedCountMarkerMalformedAndUnsupported(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		template string
		wantErr  error
	}{
		{
			name:     "malformed marker",
			template: "SELECT COUNT(*) FROM {{target} WHERE {{scope}}",
			wantErr:  errTrustedCountMalformedMarker,
		},
		{
			name:     "unsupported marker",
			template: "SELECT COUNT(*) FROM {{foo}} WHERE {{scope}}",
			wantErr:  errTrustedCountUnsupportedMarker,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := preflightTrustedCount(tc.template, nil)
			if err == nil {
				t.Fatal("expected preflight error")
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) {
				t.Fatalf("category = %v, want invalid_config", err)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestTrustedCountPlaceholderBeforeScope(t *testing.T) {
	t.Parallel()

	template := "SELECT COUNT(*) FROM {{target}} WHERE status = ? AND {{scope}}"
	err := preflightTrustedCount(template, []any{"active"})
	if err == nil {
		t.Fatal("expected preflight error")
	}
	if !errors.Is(err, errTrustedCountCustomPlaceholderBeforeScope) {
		t.Fatalf("error = %v, want %v", err, errTrustedCountCustomPlaceholderBeforeScope)
	}
}

func TestTrustedCountPlaceholderArityMismatch(t *testing.T) {
	t.Parallel()

	template := "SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND status = ?"
	err := preflightTrustedCount(template, nil)
	if err == nil {
		t.Fatal("expected preflight error")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v, want invalid_config", err)
	}
	if got, want := err.Error(), "gxsql: invalid_config: trusted count template has 1 placeholders but 0 values"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestTrustedCountPlaceholderLexicalRules(t *testing.T) {
	t.Parallel()

	template := "SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND note = 'what?'"
	err := preflightTrustedCount(template, nil)
	if err == nil {
		t.Fatal("expected preflight error")
	}
	if !errors.Is(err, ErrCategoryUnsupported) {
		t.Fatalf("category = %v, want unsupported", err)
	}
}

func TestTrustedCountScopeMustBeMarker(t *testing.T) {
	t.Parallel()

	exp := newCustomCountExpectation(
		"violating orders",
		"SELECT COUNT(*) FROM {{target}} WHERE tenant_id = 1",
		nil,
	)
	err := exp.preflight()
	if err == nil {
		t.Fatal("expected preflight error")
	}
	if !errors.Is(err, errTrustedCountScopeMarkerRequired) {
		t.Fatalf("error = %v, want %v", err, errTrustedCountScopeMarkerRequired)
	}
}
