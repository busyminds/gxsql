package gxsql

type sqliteDialect struct{}

// SQLite returns the SQLite [Dialect]. Identifiers are double-quoted after
// validation; placeholders are ?; string length uses LENGTH. LIKE-family checks
// are supported; regular expressions are not advertised until engine semantics
// are proven. Pair with [WithDialect] when validating SQLite tables.
func SQLite() Dialect { return sqliteDialect{} }

func (sqliteDialect) QuoteIdent(name string) (string, error) {
	if err := validateIdent(name); err != nil {
		return "", err
	}
	return `"` + name + `"`, nil
}

func (sqliteDialect) Placeholder(_ int) string {
	return "?"
}

func (sqliteDialect) StringLength(expr string) string {
	return "LENGTH(" + expr + ")"
}
func (sqliteDialect) supportsRelationalKeys() {}
