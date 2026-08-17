package gxsql

import (
	"context"
	"database/sql"
	"fmt"
	"math"
)

type frequencyBuilder struct {
	column   string
	value    any
	null     bool
	dominant bool
}

// Frequency returns a builder for one category's share of scoped rows. SQL NULL
// is a valid category and participates in the scoped denominator.
func (c ColumnBuilder) Frequency(value any) frequencyBuilder {
	return frequencyBuilder{column: c.column, value: value, null: value == nil}
}

// DominantShare returns a builder for the maximum category share. Equal maxima
// are represented by a tie count; no representative value is selected.
func (c ColumnBuilder) DominantShare() frequencyBuilder {
	return frequencyBuilder{column: c.column, dominant: true}
}

// GreaterOrEqual returns a table-level expectation that the category share is
// >= bound. bound must be finite and in [0, 1]; invalid bounds fail suite
// preflight. An empty scoped population passes vacuously. Results use
// [RowDenominatorUnavailable] and publish [ResultFacts.Frequency] or
// [ResultFacts.DominantShare].
func (b frequencyBuilder) GreaterOrEqual(bound float64) Expectation {
	return frequencyExpectation{column: b.column, value: b.value, null: b.null, dominant: b.dominant, op: ">=", lo: bound, hi: bound}
}

// LessOrEqual returns a table-level expectation that the category share is
// <= bound. bound must be finite and in [0, 1]; invalid bounds fail suite
// preflight. An empty scoped population passes vacuously. Results use
// [RowDenominatorUnavailable] and publish [ResultFacts.Frequency] or
// [ResultFacts.DominantShare].
func (b frequencyBuilder) LessOrEqual(bound float64) Expectation {
	return frequencyExpectation{column: b.column, value: b.value, null: b.null, dominant: b.dominant, op: "<=", lo: bound, hi: bound}
}

// Between returns a table-level expectation that the category share lies in
// [lo, hi] inclusive. Bounds must be finite and in [0, 1] with lo <= hi;
// invalid bounds fail suite preflight. An empty scoped population passes
// vacuously. Results use [RowDenominatorUnavailable] and publish
// [ResultFacts.Frequency] or [ResultFacts.DominantShare].
func (b frequencyBuilder) Between(lo, hi float64) Expectation {
	return frequencyExpectation{column: b.column, value: b.value, null: b.null, dominant: b.dominant, op: "between", lo: lo, hi: hi}
}

type frequencyExpectation struct {
	column   string
	value    any
	null     bool
	dominant bool
	op       string
	lo       float64
	hi       float64
}

func (e frequencyExpectation) Name() string {
	metric := "frequency"
	if e.dominant {
		metric = "dominant share"
	}
	if e.op == "between" {
		return fmt.Sprintf("%s %s in [%g,%g]", e.column, metric, e.lo, e.hi)
	}
	return fmt.Sprintf("%s %s %s %g", e.column, metric, e.op, e.lo)
}

func (e frequencyExpectation) expectationKind() ExpectationKind {
	if e.dominant {
		return KindDominantShare
	}
	return KindValueFrequency
}

func (e frequencyExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	if math.IsNaN(e.lo) || math.IsInf(e.lo, 0) || math.IsNaN(e.hi) || math.IsInf(e.hi, 0) {
		return newConfigError(fmt.Errorf("frequency bounds must be finite"))
	}
	if e.lo < 0 || e.lo > 1 || e.hi < 0 || e.hi > 1 {
		return newConfigError(fmt.Errorf("frequency bounds must be between 0 and 1"))
	}
	if e.op == "between" && e.lo > e.hi {
		return newConfigError(fmt.Errorf("frequency lower bound must not exceed upper bound"))
	}
	return nil
}

func (e frequencyExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	kind := e.expectationKind()
	res := Result{Kind: kind, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}
	bound, lower, upper := configuredFractionBounds(e.op, e.lo, e.hi)
	if e.dominant {
		res.Facts.DominantShare = &DominantShareFacts{
			ConfiguredBound: bound,
			ConfiguredLower: lower,
			ConfiguredUpper: upper,
		}
	} else {
		res.Facts.Frequency = &FrequencyFacts{
			ConfiguredValue: e.value,
			ConfiguredNull:  e.null,
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
	if e.dominant {
		maxCount, tieCount, query, args, queryErr := queryDominantCounts(ctx, db, table, opts, col, tbl)
		if queryErr != nil {
			captureDiagnostics(&res, opts, query, args)
			return res, queryErr
		}
		if maxCount == 0 {
			res.Success = true
			captureDiagnostics(&res, opts, query, args)
			return res, nil
		}
		facts := res.Facts.DominantShare
		facts.DominantCount = intFact(maxCount)
		facts.TotalCount = intFact(total)
		facts.TieCount = intFact(tieCount)
		share := float64(maxCount) / float64(total)
		facts.Share = floatFact(share)
		res.Success = compareBoundedFraction(share, e.op, e.lo, e.hi)
		res.Name = fmt.Sprintf("%s: got %g", e.Name(), share)
		captureDiagnostics(&res, opts, query, args)
		return res, nil
	}

	binder := newScopedArgBinder(opts.dialect, opts.scope)
	var pred rowPredicate
	if e.null {
		pred = rowPredicate{where: col + " IS NULL"}
	} else {
		pred = rowPredicate{where: col + " = " + binder.bind(e.value), args: binder.args}
	}
	effective, err := composeRowPredicateWithScope(opts.scope, pred, opts.dialect)
	if err != nil {
		return res, categorizeRenderError(err)
	}
	query, args := countQuery(tbl, effective.where)
	count, err := queryCount(ctx, db, opts, QueryCategoryTotalCount, tbl, effective.where, effective.args)
	if err != nil {
		captureDiagnostics(&res, opts, query, args)
		return res, err
	}
	facts := res.Facts.Frequency
	facts.ValueCount = intFact(count)
	facts.TotalCount = intFact(total)
	if total > 0 {
		share := float64(count) / float64(total)
		facts.Share = floatFact(share)
		res.Success = compareBoundedFraction(share, e.op, e.lo, e.hi)
		res.Name = fmt.Sprintf("%s: got %g", e.Name(), share)
	} else {
		res.Success = true
	}
	captureDiagnostics(&res, opts, query, args)
	return res, nil
}

func queryDominantCounts(
	ctx context.Context,
	db DB,
	table TableRef,
	opts evalOptions,
	column, renderedTable string,
) (maxCount, tieCount int, query string, args []any, err error) {
	scopePred, err := composeRowPredicateWithScope(opts.scope, rowPredicate{}, opts.dialect)
	if err != nil {
		return 0, 0, "", nil, categorizeRenderError(err)
	}
	query = fmt.Sprintf("SELECT %s, COUNT(*) FROM %s", column, renderedTable)
	if scopePred.where != "" {
		query += " WHERE " + scopePred.where
	}
	query += " GROUP BY " + column
	args = append([]any(nil), scopePred.args...)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, query, args, categorizeExecutionError(ctx, err)
	}
	var category any
	var count sql.NullInt64
	for rows.Next() {
		if err := rows.Scan(&category, &count); err != nil {
			_ = rows.Close()
			return 0, 0, query, args, categorizeScanError(ctx, err)
		}
		if !count.Valid || count.Int64 < 0 || int64(int(count.Int64)) != count.Int64 {
			_ = rows.Close()
			return 0, 0, query, args, categorizeScanError(ctx, fmt.Errorf("invalid grouped count"))
		}
		value := int(count.Int64)
		switch {
		case value > maxCount:
			maxCount, tieCount = value, 1
		case value == maxCount:
			tieCount++
		}
	}
	if err := finishRowsRead(ctx, rows); err != nil {
		return 0, 0, query, args, err
	}
	return maxCount, tieCount, query, args, nil
}
