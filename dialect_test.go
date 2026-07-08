package gxsql

import (
	"strings"
	"testing"
)

func TestPostgresQuoteIdent(t *testing.T) {
	d := Postgres()
	got, err := d.QuoteIdent("users")
	if err != nil {
		t.Fatal(err)
	}
	if got != `"users"` {
		t.Fatalf("QuoteIdent = %q, want %q", got, `"users"`)
	}

}

func TestPostgresPlaceholder(t *testing.T) {
	d := Postgres()
	if got := d.Placeholder(1); got != "$1" {
		t.Fatalf("Placeholder(1) = %q", got)
	}
	if got := d.Placeholder(3); got != "$3" {
		t.Fatalf("Placeholder(3) = %q", got)
	}
}

func TestPostgresStringLength(t *testing.T) {
	d := Postgres()
	got := d.StringLength(`"email"`)
	if got != `CHAR_LENGTH("email")` {
		t.Fatalf("StringLength = %q", got)
	}
}

func TestSQLitePlaceholder(t *testing.T) {
	d := SQLite()
	if got := d.Placeholder(1); got != "?" {
		t.Fatalf("Placeholder(1) = %q, want ?", got)
	}
}

func TestSQLiteStringLength(t *testing.T) {
	d := SQLite()
	got := d.StringLength(`"name"`)
	if got != `LENGTH("name")` {
		t.Fatalf("StringLength = %q", got)
	}
}
func TestDuckDBQuoteIdent(t *testing.T) {
	d := DuckDB()
	got, err := d.QuoteIdent("users")
	if err != nil {
		t.Fatal(err)
	}
	if got != `"users"` {
		t.Fatalf("QuoteIdent = %q, want %q", got, `"users"`)
	}
}

func TestDuckDBPlaceholder(t *testing.T) {
	d := DuckDB()
	if got := d.Placeholder(1); got != "$1" {
		t.Fatalf("Placeholder(1) = %q", got)
	}
	if got := d.Placeholder(3); got != "$3" {
		t.Fatalf("Placeholder(3) = %q", got)
	}
}

func TestDuckDBStringLength(t *testing.T) {
	d := DuckDB()
	got := d.StringLength(`"email"`)
	if got != `LENGTH("email")` {
		t.Fatalf("StringLength = %q", got)
	}
}
func TestMySQLQuoteIdent(t *testing.T) {
	d := MySQL()
	got, err := d.QuoteIdent("users")
	if err != nil {
		t.Fatal(err)
	}
	if got != "`users`" {
		t.Fatalf("QuoteIdent = %q, want %q", got, "`users`")
	}
}

func TestMySQLPlaceholder(t *testing.T) {
	d := MySQL()
	if got := d.Placeholder(1); got != "?" {
		t.Fatalf("Placeholder(1) = %q, want ?", got)
	}
	if got := d.Placeholder(3); got != "?" {
		t.Fatalf("Placeholder(3) = %q, want ?", got)
	}
}

func TestMySQLStringLength(t *testing.T) {
	d := MySQL()
	got := d.StringLength("`email`")
	if got != "CHAR_LENGTH(`email`)" {
		t.Fatalf("StringLength = %q", got)
	}
}

func TestDialectRejectsInvalidIdentifiers(t *testing.T) {
	for _, d := range []Dialect{Postgres(), SQLite(), DuckDB(), MySQL()} {
		if _, err := d.QuoteIdent(""); err == nil {
			t.Fatal("empty identifier should fail")
		}
		if _, err := d.QuoteIdent("bad-name"); err == nil {
			t.Fatal("hyphenated identifier should fail")
		}
	}
}

func TestRenderTableQualified(t *testing.T) {
	got, err := renderTable(Postgres(), SchemaTable("public", "users"))
	if err != nil {
		t.Fatal(err)
	}
	if got != `"public"."users"` {
		t.Fatalf("renderTable = %q", got)
	}
}

func TestPredicateRenderingUsesPlaceholders(t *testing.T) {
	d := Postgres()
	pred, err := orderedBetweenPredicate(d, "age", 0, 120)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pred.where, "$1") || !strings.Contains(pred.where, "$2") {
		t.Fatalf("expected numbered placeholders, got %q", pred.where)
	}
}

func TestInPredicateRenderingPostgres(t *testing.T) {
	d := Postgres()
	pred, err := inPredicate(d, "status", []any{"active", "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pred.where, "NOT IN") {
		t.Fatalf("expected NOT IN failure predicate, got %q", pred.where)
	}
}

func TestStringLenPredicateUsesDialectLength(t *testing.T) {
	pg, err := stringLenBetweenPredicate(Postgres(), "code", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pg.where, "CHAR_LENGTH") {
		t.Fatalf("postgres length expr missing: %q", pg.where)
	}

	sl, err := stringLenBetweenPredicate(SQLite(), "code", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sl.where, "LENGTH") {
		t.Fatalf("sqlite length expr missing: %q", sl.where)
	}
}
