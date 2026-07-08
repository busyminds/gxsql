package gxsql

import "fmt"

type duckdbDialect struct{}

// DuckDB returns the DuckDB [Dialect]. Identifiers are double-quoted after
// validation; placeholders are positional $n; string length uses LENGTH. Pair
// with [WithDialect] when validating DuckDB tables.
func DuckDB() Dialect { return duckdbDialect{} }

func (duckdbDialect) QuoteIdent(name string) (string, error) {
	if err := validateIdent(name); err != nil {
		return "", err
	}
	return `"` + name + `"`, nil
}

func (duckdbDialect) Placeholder(n int) string {
	return fmt.Sprintf("$%d", n)
}

func (duckdbDialect) StringLength(expr string) string {
	return "LENGTH(" + expr + ")"
}
