// Package dialect implements the built-in SQL dialect mechanics used by gxsql.
package dialect

import (
	"fmt"
	"regexp"
)

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidIdent reports whether name satisfies gxsql's built-in identifier rules.
func ValidIdent(name string) bool {
	return identRE.MatchString(name)
}

// ValidateIdent validates a SQL identifier using gxsql's built-in rules.
func ValidateIdent(name string) error {
	if name == "" {
		return fmt.Errorf("gxsql: empty identifier")
	}
	if !ValidIdent(name) {
		return fmt.Errorf("gxsql: invalid identifier %q", name)
	}
	return nil
}

// Postgres implements PostgreSQL identifier quoting, placeholders, and string length.
type Postgres struct{}

func (Postgres) QuoteIdent(name string) (string, error) {
	if err := ValidateIdent(name); err != nil {
		return "", err
	}
	return `"` + name + `"`, nil
}

func (Postgres) Placeholder(n int) string {
	return fmt.Sprintf("$%d", n)
}

func (Postgres) StringLength(expr string) string {
	return "CHAR_LENGTH(" + expr + ")"
}

// MySQL implements MySQL identifier quoting, placeholders, and string length.
type MySQL struct{}

func (MySQL) QuoteIdent(name string) (string, error) {
	if err := ValidateIdent(name); err != nil {
		return "", err
	}
	return "`" + name + "`", nil
}

func (MySQL) Placeholder(_ int) string {
	return "?"
}

func (MySQL) StringLength(expr string) string {
	return "CHAR_LENGTH(" + expr + ")"
}

// SQLite implements SQLite identifier quoting, placeholders, and string length.
type SQLite struct{}

func (SQLite) QuoteIdent(name string) (string, error) {
	if err := ValidateIdent(name); err != nil {
		return "", err
	}
	return `"` + name + `"`, nil
}

func (SQLite) Placeholder(_ int) string {
	return "?"
}

func (SQLite) StringLength(expr string) string {
	return "LENGTH(" + expr + ")"
}

// DuckDB implements DuckDB identifier quoting, placeholders, and string length.
type DuckDB struct{}

func (DuckDB) QuoteIdent(name string) (string, error) {
	if err := ValidateIdent(name); err != nil {
		return "", err
	}
	return `"` + name + `"`, nil
}

func (DuckDB) Placeholder(n int) string {
	return fmt.Sprintf("$%d", n)
}

func (DuckDB) StringLength(expr string) string {
	return "LENGTH(" + expr + ")"
}
