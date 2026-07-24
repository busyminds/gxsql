package gxsql

import (
	"errors"
	"strings"
	"testing"
)

func TestTrustedScopeCopiesValuesOnConstruction(t *testing.T) {
	caller := []any{"tenant-a", int64(42)}
	scope, err := newTrustedScope("pop", "tenant_id = ? AND active = ?", caller)
	if err != nil {
		t.Fatal(err)
	}
	caller[0] = "mutated"
	caller[1] = int64(0)

	if scope.values[0] != "tenant-a" {
		t.Fatalf("scope value[0] = %v, want tenant-a", scope.values[0])
	}
	if scope.values[1] != int64(42) {
		t.Fatalf("scope value[1] = %v, want 42", scope.values[1])
	}
}

func TestTrustedScopeCopiesByteSliceValues(t *testing.T) {
	payload := []byte("tenant-a")
	scope, err := newTrustedScope("pop", "tenant_id = ?", []any{payload})
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 'x'

	got, ok := scope.values[0].([]byte)
	if !ok {
		t.Fatalf("scope value type = %T, want []byte", scope.values[0])
	}
	if string(got) != "tenant-a" {
		t.Fatalf("scope value = %q, want tenant-a", got)
	}
}

func TestTrustedScopePublicConstructorDefersValidation(t *testing.T) {
	payload := []byte("tenant-a")
	args := []any{payload}
	scope := TrustedScope("   ", "data ?? ?", args...)

	if scope.identity != "   " {
		t.Fatalf("scope identity = %q, want unvalidated identity", scope.identity)
	}
	if scope.predicate != "data ?? ?" {
		t.Fatalf("scope predicate = %q, want unvalidated predicate", scope.predicate)
	}
	args[0] = "mutated"
	payload[0] = 'x'
	stored, ok := scope.values[0].([]byte)
	if !ok {
		t.Fatalf("scope value type = %T, want []byte", scope.values[0])
	}
	if string(stored) != "tenant-a" {
		t.Fatalf("scope value = %q, want tenant-a", stored)
	}
}

func TestTrustedScopeRejectsBlankIdentity(t *testing.T) {
	_, err := newTrustedScope("   ", "active = ?", []any{true})
	if err == nil {
		t.Fatal("expected blank identity rejection")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("expected invalid_config, got %v", err)
	}
	if !errors.Is(err, errScopeIdentityRequired) {
		t.Fatalf("expected scope identity error, got %v", err)
	}
}

func TestTrustedScopeRejectsValuesWithoutPredicate(t *testing.T) {
	_, err := newTrustedScope("pop", "   ", []any{"x"})
	if err == nil {
		t.Fatal("expected values-without-predicate rejection")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("expected invalid_config, got %v", err)
	}
	if !errors.Is(err, errScopeValuesWithoutPredicate) {
		t.Fatalf("expected values-without-predicate error, got %v", err)
	}
}

func TestTrustedScopeRejectsMissingPredicate(t *testing.T) {
	_, err := newTrustedScope("pop", "", nil)
	if err == nil {
		t.Fatal("expected missing predicate rejection")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("expected invalid_config, got %v", err)
	}
	if !errors.Is(err, errScopePredicateRequired) {
		t.Fatalf("expected predicate required error, got %v", err)
	}
}

func TestTrustedScopeArityMismatchRejectsBeforeRender(t *testing.T) {
	cases := []struct {
		name   string
		values []any
	}{
		{name: "one extra value", values: []any{"a", "b"}},
		{name: "one missing value", values: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTrustedScope("pop", "tenant_id = ?", tc.values)
			if err == nil {
				t.Fatal("expected arity mismatch")
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) {
				t.Fatalf("expected invalid_config, got %v", err)
			}
			if !strings.Contains(err.Error(), "1 placeholders but") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestTrustedScopeRenderPlaceholders(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		want    string
	}{
		{name: "postgres", dialect: Postgres(), want: "tenant_id = $1 AND region = $2"},
		{name: "sqlite", dialect: SQLite(), want: "tenant_id = ? AND region = ?"},
		{name: "mysql", dialect: MySQL(), want: "tenant_id = ? AND region = ?"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := newTrustedScope("pop", "tenant_id = ? AND region = ?", []any{"t1", "us"})
			if err != nil {
				t.Fatal(err)
			}
			pred, err := scope.render(tc.dialect)
			if err != nil {
				t.Fatal(err)
			}
			if pred.where != tc.want {
				t.Fatalf("where = %q, want %q", pred.where, tc.want)
			}
		})
	}
}

func TestComposeScopedPredicateParenthesizes(t *testing.T) {
	scope, err := newTrustedScope("pop", "tenant_id = ?", []any{"t1"})
	if err != nil {
		t.Fatal(err)
	}
	b := newScopedArgBinder(Postgres(), &scope)
	placeholder := b.bind(18)
	exp := withWhere(`"age" IS NULL OR "age" <= `+placeholder, b.args)

	composed, err := composeRowPredicateWithScope(&scope, exp, Postgres())
	if err != nil {
		t.Fatal(err)
	}
	want := `(tenant_id = $1) AND ("age" IS NULL OR "age" <= $2)`
	if composed.where != want {
		t.Fatalf("where = %q, want %q", composed.where, want)
	}
}

func TestComposeScopedPredicateArgsOrder(t *testing.T) {
	scope, err := newTrustedScope("pop", "tenant_id = ?", []any{"t1"})
	if err != nil {
		t.Fatal(err)
	}
	b := newScopedArgBinder(Postgres(), &scope)
	placeholder := b.bind(18)
	exp := withWhere(`"age" IS NULL OR "age" <= `+placeholder, b.args)

	composed, err := composeRowPredicateWithScope(&scope, exp, Postgres())
	if err != nil {
		t.Fatal(err)
	}
	if len(composed.args) != 2 {
		t.Fatalf("args len = %d, want 2", len(composed.args))
	}
	if composed.args[0] != "t1" {
		t.Fatalf("scope arg = %v, want t1", composed.args[0])
	}
	if composed.args[1] != 18 {
		t.Fatalf("expectation arg = %v, want 18", composed.args[1])
	}
}

func TestComposeScopedPredicateWithEmptyExpectation(t *testing.T) {
	scope, err := newTrustedScope("pop", "tenant_id = ?", []any{"t1"})
	if err != nil {
		t.Fatal(err)
	}

	composed, err := composeRowPredicateWithScope(&scope, rowPredicate{}, Postgres())
	if err != nil {
		t.Fatal(err)
	}
	if composed.where != "(tenant_id = $1)" {
		t.Fatalf("where = %q, want %q", composed.where, "(tenant_id = $1)")
	}
	if len(composed.args) != 1 || composed.args[0] != "t1" {
		t.Fatalf("args = %v, want [t1]", composed.args)
	}
}

func TestComposeScopedPredicateNilScopeReturnsOriginal(t *testing.T) {
	exp, err := orderedComparePredicate(Postgres(), "age", ">", 18, nil)
	if err != nil {
		t.Fatal(err)
	}

	composed, err := composeRowPredicateWithScope(nil, exp, Postgres())
	if err != nil {
		t.Fatal(err)
	}
	if composed.where != exp.where {
		t.Fatalf("where changed: %q vs %q", composed.where, exp.where)
	}
	if len(composed.args) != len(exp.args) || composed.args[0] != exp.args[0] {
		t.Fatalf("args changed: %v vs %v", composed.args, exp.args)
	}
}

func TestTrustedScopeUnsupportedLiteralQuestionMark(t *testing.T) {
	_, err := newTrustedScope("pop", "note = 'what?'", nil)
	if err == nil {
		t.Fatal("expected unsupported literal ?")
	}
	if !errors.Is(err, ErrCategoryUnsupported) {
		t.Fatalf("expected unsupported category, got %v", err)
	}
}

func TestTrustedScopeUnsupportedQuestionMarkOperator(t *testing.T) {
	_, err := newTrustedScope("pop", "data ?? ?", []any{"x"})
	if err == nil {
		t.Fatal("expected unsupported ? operator")
	}
	if !errors.Is(err, ErrCategoryUnsupported) {
		t.Fatalf("expected unsupported category, got %v", err)
	}
}

func TestTrustedScopeRejectsQuestionMarksOutsideSlots(t *testing.T) {
	cases := []struct {
		name      string
		predicate string
	}{
		{name: "double-quoted literal", predicate: `note = "what?"`},
		{name: "dollar-quoted literal", predicate: `note = $$what?$$`},
		{name: "json operator", predicate: `payload @? '$.active'`},
		{name: "json key operator", predicate: `payload ? 'active'`},
		{name: "mysql hash comment", predicate: `tenant_id = ? # audit ?`},
		{name: "line comment", predicate: "tenant_id = ? -- audit ?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTrustedScope("pop", tc.predicate, nil)
			if err == nil {
				t.Fatal("expected unsupported question mark")
			}
			if !errors.Is(err, ErrCategoryUnsupported) {
				t.Fatalf("expected unsupported category, got %v", err)
			}
		})
	}
}

func TestComposeScopedPredicateSQLiteArgsOrder(t *testing.T) {
	scope, err := newTrustedScope("pop", "tenant_id = ?", []any{"t1"})
	if err != nil {
		t.Fatal(err)
	}
	exp, err := orderedComparePredicate(SQLite(), "age", ">", 18, nil)
	if err != nil {
		t.Fatal(err)
	}

	composed, err := composeRowPredicateWithScope(&scope, exp, SQLite())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(composed.where, "(tenant_id = ?) AND (") {
		t.Fatalf("expected parenthesized scope, got %q", composed.where)
	}
	if len(composed.args) != 2 || composed.args[0] != "t1" || composed.args[1] != 18 {
		t.Fatalf("args = %v", composed.args)
	}
}

func TestTrustedScopeStoresTrimmedIdentity(t *testing.T) {
	scope, err := newTrustedScope("  pop  ", "tenant_id = ?", []any{"t1"})
	if err != nil {
		t.Fatal(err)
	}
	if scope.identity != "pop" {
		t.Fatalf("identity = %q, want pop", scope.identity)
	}
}

func TestTrustedScopeRenderEscapedQuoteBeforeRealSlot(t *testing.T) {
	scope, err := newTrustedScope("pop", `label = '\'' AND tenant_id = ?`, []any{"t1"})
	if err != nil {
		t.Fatal(err)
	}
	pred, err := scope.render(Postgres())
	if err != nil {
		t.Fatal(err)
	}
	want := `label = '\'' AND tenant_id = $1`
	if pred.where != want {
		t.Fatalf("where = %q, want %q", pred.where, want)
	}
	if len(pred.args) != 1 || pred.args[0] != "t1" {
		t.Fatalf("args = %v", pred.args)
	}
}

func TestNeutralLexicalWalk(t *testing.T) {
	cases := []struct {
		name       string
		fragment   string
		wantCount  int
		wantErr    bool
		wantRender string
	}{
		{
			name:       "mysql escaped quote before real slot",
			fragment:   `label = '\'' AND tenant_id = ?`,
			wantCount:  1,
			wantRender: `label = '\'' AND tenant_id = $1`,
		},
		{
			name:       "backslash escaped quote in mysql string",
			fragment:   `name = 'O\'Brien' AND tenant_id = ?`,
			wantCount:  1,
			wantRender: `name = 'O\'Brien' AND tenant_id = $1`,
		},
		{
			name:      "literal question mark in single quotes",
			fragment:  "note = 'what?'",
			wantErr:   true,
			wantCount: 0,
		},
		{
			name:       "dollar-quoted literal preserved",
			fragment:   `note = $$x$$ AND tenant_id = ?`,
			wantCount:  1,
			wantRender: `note = $$x$$ AND tenant_id = $1`,
		},
		{
			name:       "dollar tag inside identifier",
			fragment:   `tenant$id$foo = ?`,
			wantCount:  1,
			wantRender: `tenant$id$foo = $1`,
		},
		{
			name:       "slot before unterminated dollar quote tail",
			fragment:   `tenant_id = ? AND note = $tag$foo`,
			wantCount:  1,
			wantRender: `tenant_id = $1 AND note = $tag$foo`,
		},
		{
			name:       "unterminated dollar quote without question mark preserves tail",
			fragment:   `note = $tag$foo`,
			wantCount:  0,
			wantRender: `note = $tag$foo`,
		},
		{
			name:      "unterminated dollar quote with literal question mark",
			fragment:  `note = $tag$foo ?`,
			wantErr:   true,
			wantCount: 0,
		},
		{
			name:      "json key operator rejected",
			fragment:  `payload ? 'active'`,
			wantErr:   true,
			wantCount: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count, err := scanNeutralSlots(tc.fragment)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected scan error")
				}
				if !errors.Is(err, ErrCategoryUnsupported) {
					t.Fatalf("expected unsupported category, got %v", err)
				}
				_, renderErr := renderNeutralPredicate(Postgres(), tc.fragment)
				if renderErr == nil {
					t.Fatal("expected render error")
				}
				if !errors.Is(renderErr, ErrCategoryUnsupported) {
					t.Fatalf("expected unsupported category from render, got %v", renderErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if count != tc.wantCount {
				t.Fatalf("count = %d, want %d", count, tc.wantCount)
			}
			if tc.wantRender == "" {
				return
			}
			rendered, err := renderNeutralPredicate(Postgres(), tc.fragment)
			if err != nil {
				t.Fatal(err)
			}
			if rendered != tc.wantRender {
				t.Fatalf("rendered = %q, want %q", rendered, tc.wantRender)
			}
		})
	}
}
