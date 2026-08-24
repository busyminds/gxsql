package gxsql

import "github.com/busyminds/gxsql/internal/dialect"

type postgresDialect struct{}

// Postgres returns the PostgreSQL [Dialect]. Identifiers are double-quoted after
// validation; placeholders are positional $n; string length uses CHAR_LENGTH.
// It advertises [RegexDialect] with the POSIX "~" substring operator,
// [AggregateMetricsDialect] with exact STDDEV_POP, and [SchemaMetadataDialect]
// with exact reported-type support (catalog nullability is not advertised).
// Pair with [WithDialect] when validating PostgreSQL tables.
func Postgres() Dialect { return postgresDialect{} }

func (postgresDialect) supportsRelationalKeys() {}

func (postgresDialect) QuoteIdent(name string) (string, error) {
	return (dialect.Postgres{}).QuoteIdent(name)
}

func (postgresDialect) Placeholder(n int) string {
	return (dialect.Postgres{}).Placeholder(n)
}

func (postgresDialect) StringLength(expr string) string {
	return (dialect.Postgres{}).StringLength(expr)
}

type mysqlDialect struct{}

// MySQL returns the MySQL [Dialect]. Identifiers are backtick-quoted after
// validation; placeholders are ?; string length uses CHAR_LENGTH. It
// advertises [RegexDialect] with the REGEXP substring operator,
// [AggregateMetricsDialect] with exact STDDEV_POP, and [SchemaMetadataDialect]
// with catalog nullability and exact reported-type support. Pair with
// [WithDialect] when validating MySQL tables.
func MySQL() Dialect { return mysqlDialect{} }

func (mysqlDialect) supportsRelationalKeys() {}

func (mysqlDialect) QuoteIdent(name string) (string, error) {
	return (dialect.MySQL{}).QuoteIdent(name)
}

func (mysqlDialect) Placeholder(n int) string {
	return (dialect.MySQL{}).Placeholder(n)
}

func (mysqlDialect) StringLength(expr string) string {
	return (dialect.MySQL{}).StringLength(expr)
}

type sqliteDialect struct{}

// SQLite returns the SQLite [Dialect]. Identifiers are double-quoted after
// validation; placeholders are ?; string length uses LENGTH. LIKE-family checks
// are supported; regular expressions are not advertised until engine semantics
// are proven. It advertises [SchemaMetadataDialect] with exact reported-type
// support (catalog nullability is not advertised) and [AggregateMetricsDialect]
// without population standard deviation. Pair with [WithDialect] when
// validating SQLite tables.
func SQLite() Dialect { return sqliteDialect{} }

func (sqliteDialect) supportsRelationalKeys() {}

func (sqliteDialect) QuoteIdent(name string) (string, error) {
	return (dialect.SQLite{}).QuoteIdent(name)
}

func (sqliteDialect) Placeholder(n int) string {
	return (dialect.SQLite{}).Placeholder(n)
}

func (sqliteDialect) StringLength(expr string) string {
	return (dialect.SQLite{}).StringLength(expr)
}

type duckdbDialect struct{}

// DuckDB returns the DuckDB [Dialect]. Identifiers are double-quoted after
// validation; placeholders are positional $n; string length uses LENGTH. It
// advertises [RegexDialect] with the "~" substring operator,
// [AggregateMetricsDialect] with exact STDDEV_POP, and [SchemaMetadataDialect]
// with exact reported-type support (catalog nullability is not advertised).
// Pair with [WithDialect] when validating DuckDB tables.
func DuckDB() Dialect { return duckdbDialect{} }

func (duckdbDialect) supportsRelationalKeys() {}

func (duckdbDialect) QuoteIdent(name string) (string, error) {
	return (dialect.DuckDB{}).QuoteIdent(name)
}

func (duckdbDialect) Placeholder(n int) string {
	return (dialect.DuckDB{}).Placeholder(n)
}

func (duckdbDialect) StringLength(expr string) string {
	return (dialect.DuckDB{}).StringLength(expr)
}
