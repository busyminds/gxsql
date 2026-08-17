package gxsql

import "fmt"

type postgresDialect struct{}

// Postgres returns the PostgreSQL [Dialect]. Identifiers are double-quoted after
// validation; placeholders are positional $n; string length uses CHAR_LENGTH.
// It advertises [RegexDialect] with the POSIX "~" substring operator,
// [AggregateMetricsDialect] with exact STDDEV_POP, and [SchemaMetadataDialect]
// with exact reported-type support (catalog nullability is not advertised).
// Pair with [WithDialect] when validating PostgreSQL tables.
func Postgres() Dialect { return postgresDialect{} }

func (postgresDialect) QuoteIdent(name string) (string, error) {
	if err := validateIdent(name); err != nil {
		return "", err
	}
	return `"` + name + `"`, nil
}

func (postgresDialect) Placeholder(n int) string {
	return fmt.Sprintf("$%d", n)
}

func (postgresDialect) StringLength(expr string) string {
	return "CHAR_LENGTH(" + expr + ")"
}
func (postgresDialect) supportsRelationalKeys() {}
