package gxsql

import (
	"context"
	"fmt"
	"math"
)

type rateMetricBuilder struct {
	column       string
	completeness bool
}

// CompletenessRate returns a builder for the non-NULL share of scoped rows.
// Its facts are distinct from NotNull FailedPercent.
func (c ColumnBuilder) CompletenessRate() rateMetricBuilder {
	return rateMetricBuilder{column: c.column, completeness: true}
}

// DuplicateRate returns a builder for duplicate rows divided by scoped rows.
// NULL values do not participate in duplicate groups.
func (c ColumnBuilder) DuplicateRate() rateMetricBuilder {
	return rateMetricBuilder{column: c.column}
}

// GreaterOrEqual returns a table-level expectation that the rate is >= bound.
// bound must be finite and in [0, 1]; invalid bounds fail suite preflight.
// An empty scoped population passes vacuously. Results use
// [RowDenominatorUnavailable] and publish [ResultFacts.Completeness] or
// [ResultFacts.DuplicateRate].
func (b rateMetricBuilder) GreaterOrEqual(bound float64) Expectation {
	return rateExpectation{column: b.column, completeness: b.completeness, op: ">=", lo: bound, hi: bound}
}

// LessOrEqual returns a table-level expectation that the rate is <= bound.
// bound must be finite and in [0, 1]; invalid bounds fail suite preflight.
// An empty scoped population passes vacuously. Results use
// [RowDenominatorUnavailable] and publish [ResultFacts.Completeness] or
// [ResultFacts.DuplicateRate].
func (b rateMetricBuilder) LessOrEqual(bound float64) Expectation {
	return rateExpectation{column: b.column, completeness: b.completeness, op: "<=", lo: bound, hi: bound}
}

// Between returns a table-level expectation that the rate lies in [lo, hi]
// inclusive. Bounds must be finite and in [0, 1] with lo <= hi; invalid bounds
// fail suite preflight. An empty scoped population passes vacuously. Results
// use [RowDenominatorUnavailable] and publish [ResultFacts.Completeness] or
// [ResultFacts.DuplicateRate].
func (b rateMetricBuilder) Between(lo, hi float64) Expectation {
	return rateExpectation{column: b.column, completeness: b.completeness, op: "between", lo: lo, hi: hi}
}

type rateExpectation struct {
	column       string
	completeness bool
	op           string
	lo           float64
	hi           float64
}

func (e rateExpectation) Name() string {
	metric := "duplicate rate"
	if e.completeness {
		metric = "completeness rate"
	}
	if e.op == "between" {
		return fmt.Sprintf("%s %s in [%g,%g]", e.column, metric, e.lo, e.hi)
	}
	return fmt.Sprintf("%s %s %s %g", e.column, metric, e.op, e.lo)
}

func (e rateExpectation) expectationKind() ExpectationKind {
	if e.completeness {
		return KindCompletenessRate
	}
	return KindDuplicateRate
}

func (e rateExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	if math.IsNaN(e.lo) || math.IsInf(e.lo, 0) || math.IsNaN(e.hi) || math.IsInf(e.hi, 0) {
		return newConfigError(fmt.Errorf("rate bounds must be finite"))
	}
	if e.lo < 0 || e.lo > 1 || e.hi < 0 || e.hi > 1 {
		return newConfigError(fmt.Errorf("rate bounds must be between 0 and 1"))
	}
	if e.op == "between" && e.lo > e.hi {
		return newConfigError(fmt.Errorf("rate lower bound must not exceed upper bound"))
	}
	return nil
}

func (e rateExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	kind := e.expectationKind()
	res := Result{Kind: kind, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}
	bound, lower, upper := configuredFractionBounds(e.op, e.lo, e.hi)
	if e.completeness {
		res.Facts.Completeness = &CompletenessFacts{
			ConfiguredBound: bound,
			ConfiguredLower: lower,
			ConfiguredUpper: upper,
		}
	} else {
		res.Facts.DuplicateRate = &DuplicateRateFacts{
			ConfiguredBound: bound,
			ConfiguredLower: lower,
			ConfiguredUpper: upper,
		}
	}

	tbl, err := renderTable(opts.dialect, table)
	if err != nil {
		return res, categorizeRenderError(err)
	}
	col, err := quoteIdent(opts.dialect, e.column)
	if err != nil {
		return res, categorizeRenderError(err)
	}
	total, err := resolveScopedTotal(ctx, db, table, opts)
	if err != nil {
		return res, err
	}
	var numerator int
	var query string
	var args []any
	if e.completeness {
		pred, predErr := composeRowPredicateWithScope(opts.scope, rowPredicate{where: col + " IS NOT NULL"}, opts.dialect)
		if predErr != nil {
			return res, categorizeRenderError(predErr)
		}
		query, args = countQuery(tbl, pred.where)
		numerator, err = queryCount(ctx, db, opts, QueryCategoryTotalCount, tbl, pred.where, pred.args)
	} else {
		dupPred, predErr := duplicateValuePredicateWithScope(tbl, col, opts.dialect, opts.scope)
		if predErr != nil {
			return res, categorizeRenderError(predErr)
		}
		failPred, predErr := composeRowPredicateWithScope(opts.scope, dupPred, opts.dialect)
		if predErr != nil {
			return res, categorizeRenderError(predErr)
		}
		query, args = countQuery(tbl, failPred.where)
		numerator, err = queryCount(ctx, db, opts, QueryCategoryUniqueness, tbl, failPred.where, failPred.args)
	}
	if err != nil {
		captureDiagnostics(&res, opts, query, args)
		return res, err
	}
	if e.completeness {
		res.Facts.Completeness.NonNullCount = intFact(numerator)
		res.Facts.Completeness.TotalCount = intFact(total)
		if total > 0 {
			rate := float64(numerator) / float64(total)
			res.Facts.Completeness.Rate = floatFact(rate)
			res.Success = compareBoundedFraction(rate, e.op, e.lo, e.hi)
			res.Name = fmt.Sprintf("%s: got %g", e.Name(), rate)
		} else {
			res.Success = true
		}
	} else {
		res.Facts.DuplicateRate.DuplicateCount = intFact(numerator)
		res.Facts.DuplicateRate.TotalCount = intFact(total)
		if total > 0 {
			rate := float64(numerator) / float64(total)
			res.Facts.DuplicateRate.Rate = floatFact(rate)
			res.Success = compareBoundedFraction(rate, e.op, e.lo, e.hi)
			res.Name = fmt.Sprintf("%s: got %g", e.Name(), rate)
		} else {
			res.Success = true
		}
	}
	captureDiagnostics(&res, opts, query, args)
	return res, nil
}

func configuredFractionBounds(op string, lo, hi float64) (bound, lower, upper *float64) {
	if op == "between" {
		return nil, floatFact(lo), floatFact(hi)
	}
	return floatFact(lo), nil, nil
}

func compareBoundedFraction(value float64, op string, lower, upper float64) bool {
	switch op {
	case ">=":
		return value >= lower
	case "<=":
		return value <= lower
	case "between":
		return value >= lower && value <= upper
	default:
		return false
	}
}
