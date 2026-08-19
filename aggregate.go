package gxsql

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// SumBetween returns a table-level expectation that SUM(column) lies in
// [lo, hi] (inclusive). Int columns require integer bounds and use an exact
// integer result path. Float columns require finite numeric bounds and compare
// documented float64 observations. SQL NULL values are excluded. Empty or
// all-NULL input passes with no observed sum fact.
func (c NumberColumn) SumBetween(lo, hi any) Expectation {
	e := sumExpectation{column: c.column, integer: c.integer}
	if c.integer {
		e.intLo, e.intLoOK = integerBound(lo)
		e.intHi, e.intHiOK = integerBound(hi)
	} else {
		e.floatLo, e.floatLoOK = floatBound(lo)
		e.floatHi, e.floatHiOK = floatBound(hi)
	}
	return e
}

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
	observed, ok, query, args, err := queryAggregateFloatWithArgs(ctx, db, table, opts, e.column, e.agg)
	if err != nil {
		res := Result{Kind: KindAverageBetween, Name: e.label, Column: e.column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, query, args)
		var ce *CategorizedError
		if errors.As(err, &ce) {
			return res, err
		}
		return res, categorizeExecutionError(ctx, err)
	}
	if !ok {
		res := tableLevelResult(KindAverageBetween, e.column, e.label, true, configured)
		captureDiagnostics(&res, opts, query, args)
		return res, nil
	}
	name := fmt.Sprintf("%s: got %g", e.label, observed)
	success := observed >= e.lo && observed <= e.hi
	facts := configured
	facts.ObservedFloat = floatFact(observed)
	res := tableLevelResult(KindAverageBetween, e.column, name, success, facts)
	captureDiagnostics(&res, opts, query, args)
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
	observed, ok, query, args, err := queryAggregateFloatWithArgs(ctx, db, table, opts, e.column, e.agg)
	if err != nil {
		res := Result{Kind: kind, Name: e.label, Column: e.column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, query, args)
		var ce *CategorizedError
		if errors.As(err, &ce) {
			return res, err
		}
		return res, categorizeExecutionError(ctx, err)
	}
	if !ok {
		res := tableLevelResult(kind, e.column, e.label, true, configured)
		captureDiagnostics(&res, opts, query, args)
		return res, nil
	}
	name := fmt.Sprintf("%s: got %g", e.label, observed)
	success := compareAggregate(observed, e.op, e.bound)
	facts := configured
	facts.ObservedFloat = floatFact(observed)
	res := tableLevelResult(kind, e.column, name, success, facts)
	captureDiagnostics(&res, opts, query, args)
	return res, nil
}

type sumExpectation struct {
	column    string
	integer   bool
	intLo     int
	intHi     int
	intLoOK   bool
	intHiOK   bool
	floatLo   float64
	floatHi   float64
	floatLoOK bool
	floatHiOK bool
}

func (e sumExpectation) Name() string {
	if e.integer {
		return fmt.Sprintf("%s sum in [%d,%d]", e.column, e.intLo, e.intHi)
	}
	return fmt.Sprintf("%s sum in [%g,%g]", e.column, e.floatLo, e.floatHi)
}

func (e sumExpectation) expectationKind() ExpectationKind { return KindSumBetween }

func (e sumExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	if e.integer {
		if !e.intLoOK || !e.intHiOK {
			return newConfigError(fmt.Errorf("sum bounds must be integers"))
		}
		if e.intLo > e.intHi {
			return newConfigError(fmt.Errorf("sum lower bound must not exceed upper bound"))
		}
		return nil
	}
	if !e.floatLoOK || !e.floatHiOK {
		return newConfigError(fmt.Errorf("sum bounds must be finite numbers"))
	}
	if e.floatLo > e.floatHi {
		return newConfigError(fmt.Errorf("sum lower bound must not exceed upper bound"))
	}
	return nil
}

func (e sumExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	facts := ResultFacts{Sum: &SumFacts{}}
	if e.integer {
		facts.Sum.ConfiguredLower = intFact(e.intLo)
		facts.Sum.ConfiguredUpper = intFact(e.intHi)
		facts.Sum.Exactness = "exact_integer"
	} else {
		facts.Sum.ConfiguredFloatLower = floatFact(e.floatLo)
		facts.Sum.ConfiguredFloatUpper = floatFact(e.floatHi)
		facts.Sum.Exactness = "float64"
	}
	if e.integer {
		observed, ok, query, args, err := queryAggregateIntWithArgs(ctx, db, table, opts, e.column, "SUM")
		if err != nil {
			res := tableLevelResult(KindSumBetween, e.column, e.Name(), false, facts)
			captureDiagnostics(&res, opts, query, args)
			return res, err
		}
		if !ok {
			res := tableLevelResult(KindSumBetween, e.column, e.Name(), true, facts)
			captureDiagnostics(&res, opts, query, args)
			return res, nil
		}
		value := int(observed)
		if int64(value) != observed {
			err := &CategorizedError{
				Category: CategoryDatabase,
				Err:      fmt.Errorf("integer SUM overflows Go int"),
			}
			res := tableLevelResult(KindSumBetween, e.column, e.Name(), false, facts)
			captureDiagnostics(&res, opts, query, args)
			return res, err
		}
		facts.Sum.Observed = intFact(value)
		name := fmt.Sprintf("%s: got %d", e.Name(), value)
		res := tableLevelResult(KindSumBetween, e.column, name, value >= e.intLo && value <= e.intHi, facts)
		captureDiagnostics(&res, opts, query, args)
		return res, nil
	}

	observed, ok, query, args, err := queryAggregateFloatWithArgs(ctx, db, table, opts, e.column, "SUM")
	if err != nil {
		res := tableLevelResult(KindSumBetween, e.column, e.Name(), false, facts)
		captureDiagnostics(&res, opts, query, args)
		return res, err
	}
	if !ok {
		res := tableLevelResult(KindSumBetween, e.column, e.Name(), true, facts)
		captureDiagnostics(&res, opts, query, args)
		return res, nil
	}
	if math.IsNaN(observed) || math.IsInf(observed, 0) {
		err := &CategorizedError{
			Category: CategoryDatabase,
			Err:      fmt.Errorf("floating SUM is non-finite"),
		}
		res := tableLevelResult(KindSumBetween, e.column, e.Name(), false, facts)
		captureDiagnostics(&res, opts, query, args)
		return res, err
	}
	facts.Sum.ObservedFloat = floatFact(observed)
	name := fmt.Sprintf("%s: got %g", e.Name(), observed)
	res := tableLevelResult(KindSumBetween, e.column, name, observed >= e.floatLo && observed <= e.floatHi, facts)
	captureDiagnostics(&res, opts, query, args)
	return res, nil
}

func integerBound(value any) (int, bool) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	switch n := value.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		if n < int64(minInt) || n > int64(maxInt) {
			return 0, false
		}
		return int(n), true
	case uint:
		if uint64(n) > uint64(maxInt) {
			return 0, false
		}
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		if uint64(n) > uint64(maxInt) {
			return 0, false
		}
		return int(n), true
	case uint64:
		if n > uint64(maxInt) {
			return 0, false
		}
		return int(n), true // #nosec G115 -- n is bounded by maxInt above.
	default:
		return 0, false
	}
}

func floatBound(value any) (float64, bool) {
	var n float64
	switch value := value.(type) {
	case float32:
		n = float64(value)
	case float64:
		n = value
	case int:
		n = float64(value)
	case int8:
		n = float64(value)
	case int16:
		n = float64(value)
	case int32:
		n = float64(value)
	case int64:
		n = float64(value)
	case uint:
		n = float64(value)
	case uint8:
		n = float64(value)
	case uint16:
		n = float64(value)
	case uint32:
		n = float64(value)
	case uint64:
		n = float64(value)
	default:
		return 0, false
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0)
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
