package gxsql

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TimestampColumn is the entry point for temporal window and freshness checks on
// one timestamp/datetime column. Construct one with [Timestamp].
type TimestampColumn struct {
	column string
}

// Timestamp returns a builder for temporal checks on name. name must satisfy
// [Dialect] identifier rules; invalid identifiers fail suite preflight before
// SQL runs.
func Timestamp(name string) TimestampColumn {
	return TimestampColumn{column: name}
}

// InWindow returns a per-row expectation that start <= value < end (half-open).
// SQL NULL fails. An empty table passes vacuously. Bounds are bound time.Time
// values, never interpolated or compared as strings, and the library never
// calls database current-time functions. Results use [KindTimestampInWindow]
// and publish [ResultFacts.ConfiguredTimeStart] / [ResultFacts.ConfiguredTimeEnd].
// A zero bound, end <= start, or invalid column identifier fails suite
// preflight before SQL runs.
func (c TimestampColumn) InWindow(start, end time.Time) Expectation {
	return perRowExpectation{
		column: c.column,
		name:   fmt.Sprintf("%s in [%v,%v)", c.column, start, end),
		kind:   KindTimestampInWindow,
		facts: ResultFacts{
			ConfiguredTimeStart: timeFact(start),
			ConfiguredTimeEnd:   timeFact(end),
		},
		build: func(d Dialect, col string, scope *trustedScope) (rowPredicate, error) {
			return timestampInWindowPredicate(d, col, start, end, scope)
		},
		preflightCheck: func() error {
			if start.IsZero() {
				return newConfigError(fmt.Errorf("timestamp window start must be non-zero"))
			}
			if end.IsZero() {
				return newConfigError(fmt.Errorf("timestamp window end must be non-zero"))
			}
			if !end.After(start) {
				return newConfigError(fmt.Errorf("timestamp window end must be after start"))
			}
			return nil
		},
	}
}

// timestampInWindowPredicate builds a failing-row WHERE clause for the half-open
// window [start,end). A row fails when the column is NULL, strictly before
// start, or at/after end. Placeholders are numbered after any scope values.
func timestampInWindowPredicate(
	d Dialect, column string, start, end time.Time, scope *trustedScope,
) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	b := newScopedArgBinder(d, scope)
	pStart := b.bind(start)
	pEnd := b.bind(end)
	where := fmt.Sprintf("%s IS NULL OR %s < %s OR %s >= %s", col, col, pStart, col, pEnd)
	return withWhere(where, b.args), nil
}

// FreshSince returns a table-level expectation that MAX(column) over the scoped
// population is at least cutoff. SQL NULL values are excluded from the
// aggregate. An empty scope and a non-empty all-NULL scope fail because no
// accepted maximum exists. A maximum at or after cutoff passes, including
// values that are later than the cutoff; the library never calls database
// current-time functions. Results use [KindTimestampFreshSince], set
// [RowDenominatorUnavailable], publish [ResultFacts.ConfiguredTimeCutoff] and
// [ResultFacts.ObservedTime]/[ResultFacts.ObservedTimePresent], and capture
// only the aggregate query when [CaptureQueryDiagnostics] is enabled. A zero
// cutoff or invalid column identifier fails suite preflight before SQL runs.
func (c TimestampColumn) FreshSince(cutoff time.Time) Expectation {
	return freshSinceExpectation{
		column: c.column,
		label:  c.column + " fresh since",
		cutoff: cutoff,
	}
}

type freshSinceExpectation struct {
	column string
	label  string
	cutoff time.Time
}

func (e freshSinceExpectation) Name() string { return e.label }

func (e freshSinceExpectation) expectationKind() ExpectationKind {
	return KindTimestampFreshSince
}

func (e freshSinceExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	if e.cutoff.IsZero() {
		return newConfigError(fmt.Errorf("freshness cutoff must be non-zero"))
	}
	return nil
}

func (e freshSinceExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	configured := ResultFacts{
		ConfiguredTimeCutoff: timeFact(e.cutoff),
	}
	observed, ok, query, args, err := queryAggregateTimeWithArgs(ctx, db, table, opts, e.column, "MAX")
	if err != nil {
		res := Result{
			Kind:           KindTimestampFreshSince,
			Name:           e.label,
			Column:         e.column,
			RowDenominator: RowDenominatorUnavailable,
			Facts:          configured,
		}
		captureDiagnostics(&res, opts, query, args)
		var ce *CategorizedError
		if errors.As(err, &ce) {
			return res, err
		}
		return res, categorizeExecutionError(ctx, err)
	}
	if !ok {
		facts := configured
		facts.ObservedTimePresent = boolFact(false)
		res := tableLevelResult(KindTimestampFreshSince, e.column, e.label, false, facts)
		captureDiagnostics(&res, opts, query, args)
		return res, nil
	}
	name := fmt.Sprintf("%s: got %s", e.label, observed.UTC().Format(time.RFC3339Nano))
	facts := configured
	facts.ObservedTime = timeFact(observed)
	facts.ObservedTimePresent = boolFact(true)
	success := !observed.Before(e.cutoff)
	res := tableLevelResult(KindTimestampFreshSince, e.column, name, success, facts)
	captureDiagnostics(&res, opts, query, args)
	return res, nil
}
