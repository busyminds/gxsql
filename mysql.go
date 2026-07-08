package gxsql

type mysqlDialect struct{}

// MySQL returns the MySQL [Dialect]. Identifiers are backtick-quoted after
// validation; placeholders are ?; string length uses CHAR_LENGTH. Pair with
// [WithDialect] when validating MySQL tables.
func MySQL() Dialect { return mysqlDialect{} }

func (mysqlDialect) QuoteIdent(name string) (string, error) {
	if err := validateIdent(name); err != nil {
		return "", err
	}
	return "`" + name + "`", nil
}

func (mysqlDialect) Placeholder(_ int) string {
	return "?"
}

func (mysqlDialect) StringLength(expr string) string {
	return "CHAR_LENGTH(" + expr + ")"
}
