package gxsql

import (
	"context"
	"fmt"
	"strings"
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
	failQuery, failArgs := failedCountDiagnostics(tbl, failPred)

	total, err := resolveScopedTotal(ctx, db, table, opts)
	if err != nil {
		res := Result{Kind: KindUnique, Name: e.Name(), Column: e.column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, err
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

type compositeUniqueExpectation struct {
	columns []string
}

func (e compositeUniqueExpectation) Name() string {
	return strings.Join(e.columns, ", ") + " unique"
}

func (e compositeUniqueExpectation) expectationKind() ExpectationKind {
	return KindCompositeUnique
}

func (e compositeUniqueExpectation) preflight() error {
	if err := validateCompositeColumns(e.columns); err != nil {
		return newConfigError(err)
	}
	return nil
}

func (e compositeUniqueExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	facts := ResultFacts{KeyColumns: append([]string(nil), e.columns...)}
	base := Result{
		Kind:           KindCompositeUnique,
		Name:           e.Name(),
		RowDenominator: RowDenominatorUnavailable,
		Facts:          facts,
	}

	if !dialectSupportsRelationalKeys(opts.dialect) {
		return base, unsupportedRelationalDialectError()
	}

	tbl, err := renderTable(opts.dialect, table)
	if err != nil {
		return base, categorizeRenderError(err)
	}
	quoted, err := quoteColumns(opts.dialect, e.columns)
	if err != nil {
		return base, categorizeRenderError(err)
	}

	dupPred, err := compositeDuplicatePredicateWithScope(tbl, quoted, opts.dialect, opts.scope)
	if err != nil {
		return base, categorizeRenderError(err)
	}
	failPred, err := composeRowPredicateWithScope(opts.scope, dupPred, opts.dialect)
	if err != nil {
		return base, categorizeRenderError(err)
	}
	failQuery, failArgs := failedCountDiagnostics(tbl, failPred)

	total, err := resolveScopedTotal(ctx, db, table, opts)
	if err != nil {
		res := base
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, err
	}

	failed, err := queryCount(ctx, db, tbl, failPred.where, failPred.args)
	if err != nil {
		res := base
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, categorizeExecutionError(ctx, err)
	}

	res := perRowResult(KindCompositeUnique, "", e.Name(), total, failed, facts)
	captureDiagnostics(&res, opts, failQuery, failArgs)
	if failed == 0 {
		return res, nil
	}

	if opts.sampleCap > 0 {
		samples, err := queryColumnSamples(ctx, db, tbl, e.columns[0], failPred, opts, opts.sampleCap)
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

func validateCompositeColumns(columns []string) error {
	if len(columns) < 2 {
		return fmt.Errorf("gxsql: composite unique requires at least two columns")
	}
	if err := validateDistinctIdents(columns, "composite column"); err != nil {
		return err
	}
	return nil
}

func compositeDuplicatePredicateWithScope(table string, quotedColumns []string, d Dialect, scope *trustedScope) (rowPredicate, error) {
	nullChecks := make([]string, len(quotedColumns))
	for i, col := range quotedColumns {
		nullChecks[i] = col + " IS NOT NULL"
	}
	list := joinQuoted(quotedColumns)
	tuple := "(" + list + ")"
	prefix := strings.Join(nullChecks, " AND ")

	if scope == nil {
		where := fmt.Sprintf(
			"%s AND %s IN (SELECT %s FROM %s GROUP BY %s HAVING COUNT(*) > 1)",
			prefix, tuple, list, table, list,
		)
		return withWhere(where, nil), nil
	}

	scopePred, err := scope.renderAt(d, len(scope.values))
	if err != nil {
		return rowPredicate{}, err
	}
	where := fmt.Sprintf(
		"%s AND %s IN (SELECT %s FROM %s WHERE (%s) GROUP BY %s HAVING COUNT(*) > 1)",
		prefix, tuple, list, table, scopePred.where, list,
	)
	return withWhere(where, scopePred.args), nil
}
