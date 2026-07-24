package gxsql

import (
	"context"
	"database/sql"
)

// DefaultSampleCap is the default maximum offending sample values retained per
// failing per-row result. Override per suite with [Suite.WithSampleCap] or per
// run with [WithSampleCap]. A cap of zero disables sample collection.
const DefaultSampleCap = 20

// DB is the narrow database interface [Suite.ValidateTable] executes against.
// Implementations must honor context cancellation on [context.Context].
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Expectation is the sealed unit of SQL validation over a table. Construct
// expectations with the column, row-count, and aggregate builders; its
// unexported methods prevent implementations outside package gxsql. The
// [Name] method supplies display text, while [Suite.ValidateTable] reports a
// library-defined [Result.Kind]. Attach a stable result ID with [WithID].
type Expectation interface {
	Name() string
	evaluateSQL(ctx context.Context, db DB, table TableRef, opts evalOptions) (Result, error)
}

type evalOptions struct {
	dialect            Dialect
	sampleCap          int
	failedKeysCap      int // 0 means unlimited
	keyColumns         []string
	summaryOnly        bool
	captureDiagnostics bool
	scope              *trustedScope
	// scopedTotal caches one shared COUNT(*) for denominator-using expectations
	// within a ValidateTable call. Nil means each resolve loads locally.
	scopedTotal *scopedTotalCache
}

// scopedTotalCache holds the once-per-ValidateTable scoped total COUNT(*).
type scopedTotalCache struct {
	loaded bool
	total  int
	err    error
}

// rowPredicate is a SQL WHERE clause that is true for failing rows.
type rowPredicate struct {
	where string
	args  []any
}

type argBinder struct {
	dialect           Dialect
	args              []any
	placeholderOffset int
}

// newScopedArgBinder returns a binder whose placeholders begin after scope
// values. Expectation values bind from the next slot; scope values are
// prepended separately at composition time.
func newScopedArgBinder(d Dialect, scope *trustedScope) *argBinder {
	scopePrefix := 0
	if scope != nil {
		scopePrefix = len(scope.values)
	}
	return &argBinder{dialect: d, placeholderOffset: scopePrefix}
}

func (b *argBinder) bind(v any) string {
	b.args = append(b.args, v)
	return b.dialect.Placeholder(b.placeholderOffset + len(b.args))
}

func withWhere(where string, args []any) rowPredicate {
	return rowPredicate{where: where, args: args}
}

func perRowResult(kind ExpectationKind, column, displayName string, total, failed int, facts ResultFacts) Result {
	res := Result{
		Kind:           kind,
		Name:           displayName,
		Column:         column,
		RowDenominator: RowDenominatorAvailable,
		Total:          total,
		Success:        failed == 0,
		Facts:          facts,
	}
	if failed > 0 {
		res.FailedCount = failed
	}
	if total > 0 && failed > 0 {
		res.FailedPercent = float64(failed) / float64(total) * 100
	}
	return res
}

func intFact(n int) *int { return &n }

func floatFact(n float64) *float64 { return &n }

func tableLevelResult(kind ExpectationKind, column, displayName string, success bool, facts ResultFacts) Result {
	return Result{
		Kind:           kind,
		Name:           displayName,
		Column:         column,
		Success:        success,
		RowDenominator: RowDenominatorUnavailable,
		Facts:          facts,
	}
}
