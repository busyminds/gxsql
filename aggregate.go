package gxsql

import (
	"context"
	"errors"
	"fmt"
)

// AverageBetween returns a table-level expectation that AVG(column) lies in
// [lo, hi] (inclusive). Build it from a numeric column via [Int] or [Float].
// SQL NULL values are excluded from the aggregate. When every value is NULL the
// check passes vacuously. Results use [KindAverageBetween], set
// [RowDenominatorUnavailable], and append the observed average to Name on
// evaluation. Invalid column identifiers fail suite preflight before SQL runs.
func (c NumberColumn) AverageBetween(lo, hi float64) Expectation {
	return aggregateExpectation{
		column: c.column,
		label:  fmt.Sprintf("%s average in [%g,%g]", c.column, lo, hi),
		agg:    "AVG",
		lo:     lo,
		hi:     hi,
	}
}

// MinGreaterOrEqual returns a table-level expectation that MIN(column) >= bound.
// Build it from a numeric column via [Int] or [Float]. SQL NULL values are
// excluded. When every value is NULL the check passes vacuously. Results use
// [KindMinGreaterOrEqual], set [RowDenominatorUnavailable], and append the
// observed minimum to Name on evaluation. Invalid column identifiers fail suite
// preflight before SQL runs.
func (c NumberColumn) MinGreaterOrEqual(bound float64) Expectation {
	return aggregateBoundExpectation{
		column: c.column,
		label:  fmt.Sprintf("%s min >= %g", c.column, bound),
		agg:    "MIN",
		op:     ">=",
		bound:  bound,
	}
}

// MaxLessOrEqual returns a table-level expectation that MAX(column) <= bound.
// Build it from a numeric column via [Int] or [Float]. SQL NULL values are
// excluded. When every value is NULL the check passes vacuously. Results use
// [KindMaxLessOrEqual], set [RowDenominatorUnavailable], and append the
// observed maximum to Name on evaluation. Invalid column identifiers fail suite
// preflight before SQL runs.
func (c NumberColumn) MaxLessOrEqual(bound float64) Expectation {
	return aggregateBoundExpectation{
		column: c.column,
		label:  fmt.Sprintf("%s max <= %g", c.column, bound),
		agg:    "MAX",
		op:     "<=",
		bound:  bound,
	}
}

type aggregateExpectation struct {
	column string
	label  string
	agg    string
	lo, hi float64
}

func (e aggregateExpectation) Name() string { return e.label }

func (e aggregateExpectation) expectationKind() ExpectationKind { return KindAverageBetween }

func (e aggregateExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	return nil
}

func (e aggregateExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	configured := ResultFacts{
		ConfiguredFloatLower: floatFact(e.lo),
		ConfiguredFloatUpper: floatFact(e.hi),
	}
	observed, ok, query, err := queryAggregateFloat(ctx, db, table, opts, e.column, e.agg)
	if err != nil {
		res := Result{Kind: KindAverageBetween, Name: e.label, Column: e.column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, query, nil)
		var ce *CategorizedError
		if errors.As(err, &ce) {
			return res, err
		}
		return res, categorizeExecutionError(ctx, err)
	}
	if !ok {
		res := tableLevelResult(KindAverageBetween, e.column, e.label, true, configured)
		captureDiagnostics(&res, opts, query, nil)
		return res, nil
	}
	name := fmt.Sprintf("%s: got %g", e.label, observed)
	success := observed >= e.lo && observed <= e.hi
	facts := configured
	facts.ObservedFloat = floatFact(observed)
	res := tableLevelResult(KindAverageBetween, e.column, name, success, facts)
	captureDiagnostics(&res, opts, query, nil)
	return res, nil
}

type aggregateBoundExpectation struct {
	column string
	label  string
	agg    string
	op     string
	bound  float64
}

func (e aggregateBoundExpectation) Name() string { return e.label }

func (e aggregateBoundExpectation) expectationKind() ExpectationKind {
	switch e.agg {
	case "MIN":
		return KindMinGreaterOrEqual
	case "MAX":
		return KindMaxLessOrEqual
	default:
		return KindCustom
	}
}

func (e aggregateBoundExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	return nil
}

func (e aggregateBoundExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	kind := e.expectationKind()
	configured := ResultFacts{ConfiguredFloatBound: floatFact(e.bound)}
	observed, ok, query, err := queryAggregateFloat(ctx, db, table, opts, e.column, e.agg)
	if err != nil {
		res := Result{Kind: kind, Name: e.label, Column: e.column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, query, nil)
		var ce *CategorizedError
		if errors.As(err, &ce) {
			return res, err
		}
		return res, categorizeExecutionError(ctx, err)
	}
	if !ok {
		res := tableLevelResult(kind, e.column, e.label, true, configured)
		captureDiagnostics(&res, opts, query, nil)
		return res, nil
	}
	name := fmt.Sprintf("%s: got %g", e.label, observed)
	success := compareAggregate(observed, e.op, e.bound)
	facts := configured
	facts.ObservedFloat = floatFact(observed)
	res := tableLevelResult(kind, e.column, name, success, facts)
	captureDiagnostics(&res, opts, query, nil)
	return res, nil
}

func compareAggregate(observed float64, op string, bound float64) bool {
	switch op {
	case ">=":
		return observed >= bound
	case "<=":
		return observed <= bound
	default:
		return false
	}
}
