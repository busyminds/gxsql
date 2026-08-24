package gxsql

import (
	"fmt"
	"strings"

	"github.com/busyminds/gxsql/internal/dialect"
)

// Dialect renders dialect-specific SQL fragments for identifiers, bound
// parameters, and string-length expressions. Pass a concrete implementation to
// [WithDialect]. [QuoteIdent] must reject empty or invalid names. A nil dialect
// is a run-level configuration error before preflight or SQL, not a
// rendering-time error.
//
// Optional capabilities use separate interfaces rather than widening Dialect.
// Regex support is advertised through [RegexDialect]; dialects that omit it
// fail [StringColumn.Regex] closed at suite preflight. Catalog nullability and
// exact reported-type contracts are advertised through [SchemaMetadataDialect].
// Exact population-standard-deviation support is advertised through
// [AggregateMetricsDialect].
type Dialect interface {
	// QuoteIdent returns a dialect-quoted identifier for name.
	// It must reject empty or invalid names.
	QuoteIdent(name string) (string, error)
	// Placeholder returns the nth bound-parameter placeholder.
	// n is 1-based and matches the position of the corresponding argument.
	Placeholder(n int) string
	// StringLength returns a SQL expression for the character length of expr.
	StringLength(expr string) string
}

// TableRef names a database table for [Suite.ValidateTable]. Schema and Name are
// quoted separately by the active [Dialect]; raw strings are never concatenated
// into SQL unquoted.
type TableRef struct {
	// Schema is an optional schema qualifier. Empty means unqualified.
	Schema string
	// Name is the table identifier. It must satisfy identifier validation when
	// rendered.
	Name string
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
	return dialect.ValidateIdent(name)
}

func validIdent(name string) bool {
	return dialect.ValidIdent(name)
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
