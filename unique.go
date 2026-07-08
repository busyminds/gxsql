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

	pred := duplicateValuePredicate(tbl, col)
	failQuery, failArgs := failedCountDiagnostics(tbl, pred)

	total, err := queryCount(ctx, db, tbl, "", nil)
	if err != nil {
		res := Result{Kind: KindUnique, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, categorizeExecutionError(ctx, err)
	}

	failed, err := queryCount(ctx, db, tbl, pred.where, pred.args)
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
		samples, err := queryColumnSamples(ctx, db, tbl, e.column, pred, opts, opts.sampleCap)
		if err != nil {
			return res, categorizeExecutionError(ctx, err)
		}
		res.SampleValues = samples
	}

	if !opts.summaryOnly && len(opts.keyColumns) > 0 {
		keys, err := queryFailedKeys(ctx, db, tbl, opts, pred)
		if err != nil {
			return res, categorizeExecutionError(ctx, err)
		}
		res.FailedKeys = keys
	}
	return res, nil
}

func duplicateValuePredicate(table, column string) rowPredicate {
	where := fmt.Sprintf(
		"%s IS NOT NULL AND %s IN (SELECT %s FROM %s GROUP BY %s HAVING COUNT(*) > 1)",
		column, column, column, table, column,
	)
	return withWhere(where, nil)
}
