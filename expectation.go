package gxsql

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
// expectations with the RowCount, RequiredColumns, ExactColumns, column,
// aggregate, temporal, and CustomCount builders; its unexported methods prevent
// implementations outside package gxsql. The [Name] method supplies display
// text, while [Suite.ValidateTable] reports a library-defined [Result.Kind].
// Attach a stable result ID with [WithID].
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

// perRowResult builds raw per-row observations before any tolerance policy is
// applied. Success here is the raw zero-failure verdict; FailedCount,
// FailedPercent, and Total are never adjusted by later policy decoration.
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

func timeFact(t time.Time) *time.Time { return &t }

func boolFact(b bool) *bool { return &b }

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

// applyMaxFailedCount decorates a measured result with an inclusive maximum
// failed-count policy. It preserves raw Total, FailedCount, FailedPercent,
// samples, keys, identity, facts, and errors; only Success and Tolerated
// change. Negative bounds are rejected at ValidateTable preflight.
func applyMaxFailedCount(res Result, max int) Result {
	res.Facts.ConfiguredMaxFailedCount = intFact(max)
	res.Tolerated = false

	if res.Err != nil {
		res.Success = false
		return res
	}
	if res.RowDenominator != RowDenominatorAvailable {
		return res
	}
	if res.FailedCount <= max {
		res.Success = true
		res.Tolerated = res.FailedCount > 0
		return res
	}
	res.Success = false
	return res
}

// toleranceEligible is the private capability marking declarations that may
// carry a maximum-failed-count wrapper. Per-row, uniqueness, composite-key,
// and reference expectations implement it; eligibility is never inferred from
// a result.
type toleranceEligible interface {
	toleranceEligible()
}

func (perRowExpectation) toleranceEligible()          {}
func (uniqueExpectation) toleranceEligible()          {}
func (compositeUniqueExpectation) toleranceEligible() {}
func (referenceExpectation) toleranceEligible()       {}

// maxFailedCountExpectation is the immutable internal tolerance declaration
// constructed by WithMaxFailedCount.
type maxFailedCountExpectation struct {
	max   int
	inner Expectation
}

// WithMaxFailedCount wraps an eligible expectation with an inclusive maximum
// failed-row allowance. Only per-row and uniqueness expectations qualify,
// including composite uniqueness and referential integrity; table-level,
// aggregate, distinct-count, row-count, custom-count, and structural column
// declarations are rejected at ValidateTable preflight. Negative max values
// are also rejected at preflight. The wrapper is immutable and may nest with
// WithID in either order.
//
// Tolerance changes only the policy verdict. Raw Total, FailedCount,
// FailedPercent, SampleValues, and FailedKeys are preserved under their
// existing caps. A nonzero raw failure count at or below max yields Success
// true and Tolerated true; raw-zero and empty populations pass without being
// tolerated; above-bound and error results cannot be tolerated.
func WithMaxFailedCount(max int, exp Expectation) Expectation {
	return &maxFailedCountExpectation{max: max, inner: exp}
}

func (e *maxFailedCountExpectation) Name() string {
	if e.inner == nil {
		return "<configuration error>"
	}
	return e.inner.Name()
}

func (e *maxFailedCountExpectation) expectationKind() ExpectationKind {
	return expectationKind(e.inner)
}

func (e *maxFailedCountExpectation) preflight() error {
	if e.max < 0 {
		return newConfigError(fmt.Errorf("max failed count must be non-negative"))
	}
	if e.inner == nil {
		return newConfigError(fmt.Errorf("nil expectation"))
	}
	if err := rejectNestedTolerance(e.inner); err != nil {
		return err
	}
	core := unwrapIDExpectations(e.inner)
	if core == nil {
		return newConfigError(fmt.Errorf("nil expectation"))
	}
	if _, ok := core.(toleranceEligible); !ok {
		return newConfigError(fmt.Errorf("expectation does not support failure tolerance"))
	}
	return preflightExpectation(e.inner)
}

func (e *maxFailedCountExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	res, err := e.inner.evaluateSQL(ctx, db, table, opts)
	if err != nil && res.Err == nil {
		res.Err = err
	}
	return applyMaxFailedCount(res, e.max), err
}

// rejectNestedTolerance reports a configuration error when another tolerance
// wrapper appears at any nesting depth beneath WithID layers.
func rejectNestedTolerance(exp Expectation) error {
	for {
		switch w := exp.(type) {
		case *idExpectation:
			if w == nil {
				return nil
			}
			exp = w.inner
		case *maxFailedCountExpectation:
			return newConfigError(fmt.Errorf("failure tolerance already applied"))
		default:
			return nil
		}
	}
}

// unwrapIDExpectations peels WithID wrappers without removing tolerance wrappers.
func unwrapIDExpectations(exp Expectation) Expectation {
	for {
		w, ok := exp.(*idExpectation)
		if !ok || w == nil {
			return exp
		}
		exp = w.inner
	}
}
