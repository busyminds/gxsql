package gxsql

import (
	"context"
	"fmt"
)

type uniqueExpectation struct {
	column string
}

func (e uniqueExpectation) Name() string {
	return e.column + " unique"
}

func (e uniqueExpectation) expectationKind() ExpectationKind { return KindUnique }

func (e uniqueExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	return nil
}

func (e uniqueExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	tbl, err := renderTable(opts.dialect, table)
	if err != nil {
		return Result{Kind: KindUnique, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}
	col, err := quoteIdent(opts.dialect, e.column)
	if err != nil {
		return Result{Kind: KindUnique, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}

	dupPred, err := duplicateValuePredicateWithScope(tbl, col, opts.dialect, opts.scope)
	if err != nil {
		return Result{Kind: KindUnique, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}
	failPred, err := composeRowPredicateWithScope(opts.scope, dupPred, opts.dialect)
	if err != nil {
		return Result{Kind: KindUnique, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}
	totalPred, err := composeRowPredicateWithScope(opts.scope, rowPredicate{}, opts.dialect)
	if err != nil {
		return Result{Kind: KindUnique, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}
	failQuery, failArgs := failedCountDiagnostics(tbl, failPred)

	total, err := queryCount(ctx, db, tbl, totalPred.where, totalPred.args)
	if err != nil {
		res := Result{Kind: KindUnique, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, categorizeExecutionError(ctx, err)
	}

	failed, err := queryCount(ctx, db, tbl, failPred.where, failPred.args)
	if err != nil {
		res := Result{Kind: KindUnique, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, categorizeExecutionError(ctx, err)
	}

	res := perRowResult(KindUnique, e.column, e.Name(), total, failed, ResultFacts{})
	captureDiagnostics(&res, opts, failQuery, failArgs)
	if failed == 0 {
		return res, nil
	}

	if opts.sampleCap > 0 {
		samples, err := queryColumnSamples(ctx, db, tbl, e.column, failPred, opts, opts.sampleCap)
		if err != nil {
			return res, categorizeExecutionError(ctx, err)
		}
		res.SampleValues = samples
	}

	if !opts.summaryOnly && len(opts.keyColumns) > 0 {
		keys, err := queryFailedKeys(ctx, db, tbl, opts, failPred)
		if err != nil {
			return res, categorizeExecutionError(ctx, err)
		}
		res.FailedKeys = keys
	}
	return res, nil
}

func duplicateValuePredicateWithScope(table, column string, d Dialect, scope *trustedScope) (rowPredicate, error) {
	if scope == nil {
		where := fmt.Sprintf(
			"%s IS NOT NULL AND %s IN (SELECT %s FROM %s GROUP BY %s HAVING COUNT(*) > 1)",
			column, column, column, table, column,
		)
		return withWhere(where, nil), nil
	}

	scopePred, err := scope.renderAt(d, len(scope.values))
	if err != nil {
		return rowPredicate{}, err
	}
	where := fmt.Sprintf(
		"%s IS NOT NULL AND %s IN (SELECT %s FROM %s WHERE (%s) GROUP BY %s HAVING COUNT(*) > 1)",
		column, column, column, table, scopePred.where, column,
	)
	return withWhere(where, scopePred.args), nil
}
