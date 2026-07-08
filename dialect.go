package gxsql

import (
	"fmt"
	"regexp"
	"strings"
)

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Dialect renders dialect-specific SQL fragments for identifiers, bound
// parameters, and string-length expressions. Pass a concrete implementation to
// [WithDialect]. [QuoteIdent] must reject empty or invalid names. A nil dialect
// is a run-level configuration error before preflight or SQL, not a
// rendering-time error.
type Dialect interface {
	QuoteIdent(name string) (string, error)
	Placeholder(n int) string
	StringLength(expr string) string
}

// TableRef names a database table for [Suite.ValidateTable]. Schema and Name are
// quoted separately by the active [Dialect]; raw strings are never concatenated
// into SQL unquoted.
type TableRef struct {
	Schema string
	Name   string
}

// Table returns an unqualified table reference. Name must satisfy identifier
// validation when rendered.
func Table(name string) TableRef {
	return TableRef{Name: name}
}

// SchemaTable returns a schema-qualified table reference. Both schema and name
// must satisfy identifier validation when rendered.
func SchemaTable(schema, name string) TableRef {
	return TableRef{Schema: schema, Name: name}
}

func validateIdent(name string) error {
	if name == "" {
		return fmt.Errorf("gxsql: empty identifier")
	}
	if !identRE.MatchString(name) {
		return fmt.Errorf("gxsql: invalid identifier %q", name)
	}
	return nil
}

func quoteIdent(d Dialect, name string) (string, error) {
	if d == nil {
		return "", fmt.Errorf("gxsql: dialect is required")
	}
	return d.QuoteIdent(name)
}

func renderTable(d Dialect, table TableRef) (string, error) {
	name, err := quoteIdent(d, table.Name)
	if err != nil {
		return "", err
	}
	if table.Schema == "" {
		return name, nil
	}
	schema, err := quoteIdent(d, table.Schema)
	if err != nil {
		return "", err
	}
	return schema + "." + name, nil
}

func quoteColumns(d Dialect, columns []string) ([]string, error) {
	out := make([]string, len(columns))
	for i, col := range columns {
		quoted, err := quoteIdent(d, col)
		if err != nil {
			return nil, err
		}
		out[i] = quoted
	}
	return out, nil
}

func joinQuoted(columns []string) string {
	return strings.Join(columns, ", ")
}
