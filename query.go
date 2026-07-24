package gxsql

import (
	"context"
	"database/sql"
	"fmt"
)

func finishRowsRead(ctx context.Context, rows *sql.Rows) error {
	iterErr := rows.Err()
	closeErr := rows.Close()
	switch {
	case iterErr != nil:
		return categorizeScanError(ctx, iterErr)
	case closeErr != nil:
		return categorizeScanError(ctx, closeErr)
	default:
		return nil
	}
}

// queryScalarInt scans one integer. NULL is coerced to 0; callers must use it
// only for COUNT(*) and other never-null aggregates.
func queryScalarInt(ctx context.Context, db DB, query string, args ...any) (int, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, categorizeExecutionError(ctx, err)
	}

	var n sql.NullInt64
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return 0, categorizeScanError(ctx, err)
		}
		if err := rows.Close(); err != nil {
			return 0, categorizeScanError(ctx, err)
		}
		return 0, categorizeScanError(ctx, sql.ErrNoRows)
	}
	if err := rows.Scan(&n); err != nil {
		_ = rows.Close()
		return 0, categorizeScanError(ctx, err)
	}
	if err := finishRowsRead(ctx, rows); err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return int(n.Int64), nil
}

func captureDiagnostics(res *Result, opts evalOptions, query string, args []any) {
	if !opts.captureDiagnostics || res.diagnostics != nil {
		return
	}
	cp := append([]any(nil), args...)
	res.diagnostics = &resultDiagnostics{query: query, args: cp}
}

func failedCountDiagnostics(tbl string, pred rowPredicate) (string, []any) {
	query, _ := countQuery(tbl, pred.where)
	return query, append([]any(nil), pred.args...)
}

func countQuery(table, where string) (string, []any) {
	query := "SELECT COUNT(*) FROM " + table
	if where != "" {
		query += " WHERE " + where
	}
	return query, nil
}

func queryCount(ctx context.Context, db DB, table, where string, args []any) (int, error) {
	query, _ := countQuery(table, where)
	return queryScalarInt(ctx, db, query, args...)
}

// loadScopedTotal issues the shared scoped COUNT(*) for the evaluated population.
func loadScopedTotal(ctx context.Context, db DB, table TableRef, opts evalOptions) (int, error) {
	tbl, err := renderTable(opts.dialect, table)
	if err != nil {
		return 0, categorizeRenderError(err)
	}
	totalPred, err := composeRowPredicateWithScope(opts.scope, rowPredicate{}, opts.dialect)
	if err != nil {
		return 0, categorizeRenderError(err)
	}
	total, err := queryCount(ctx, db, tbl, totalPred.where, totalPred.args)
	if err != nil {
		return 0, categorizeExecutionError(ctx, err)
	}
	return total, nil
}

// resolveScopedTotal returns the scoped total row count, reusing opts.scopedTotal
// when ValidateTable attached a cache. With a nil cache, each call loads locally.
func resolveScopedTotal(ctx context.Context, db DB, table TableRef, opts evalOptions) (int, error) {
	if opts.scopedTotal == nil {
		return loadScopedTotal(ctx, db, table, opts)
	}
	c := opts.scopedTotal
	if !c.loaded {
		c.total, c.err = loadScopedTotal(ctx, db, table, opts)
		c.loaded = true
	}
	return c.total, c.err
}

func evalPerRow(
	ctx context.Context,
	db DB,
	table TableRef,
	opts evalOptions,
	kind ExpectationKind,
	displayName, column string,
	facts ResultFacts,
	pred rowPredicate,
) (Result, error) {
	tbl, err := renderTable(opts.dialect, table)
	if err != nil {
		return Result{Kind: kind, Name: displayName, Column: column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}

	if _, err := quoteIdent(opts.dialect, column); err != nil {
		return Result{Kind: kind, Name: displayName, Column: column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}

	failPred, err := composeRowPredicateWithScope(opts.scope, pred, opts.dialect)
	if err != nil {
		return Result{Kind: kind, Name: displayName, Column: column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}

	failQuery, failArgs := failedCountDiagnostics(tbl, failPred)

	total, err := resolveScopedTotal(ctx, db, table, opts)
	if err != nil {
		res := Result{Kind: kind, Name: displayName, Column: column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, err
	}

	failed, err := queryCount(ctx, db, tbl, failPred.where, failPred.args)
	if err != nil {
		res := Result{Kind: kind, Name: displayName, Column: column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, categorizeExecutionError(ctx, err)
	}

	res := perRowResult(kind, column, displayName, total, failed, facts)
	captureDiagnostics(&res, opts, failQuery, failArgs)
	if failed == 0 {
		return res, nil
	}

	if opts.sampleCap > 0 {
		samples, err := queryColumnSamples(ctx, db, tbl, column, failPred, opts, opts.sampleCap)
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

func queryColumnSamples(
	ctx context.Context,
	db DB,
	table, column string,
	pred rowPredicate,
	opts evalOptions,
	limit int,
) ([]any, error) {
	quotedColumn, err := quoteIdent(opts.dialect, column)
	if err != nil {
		return nil, categorizeRenderError(err)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", quotedColumn, table)
	if pred.where != "" {
		query += " WHERE " + pred.where
	}

	orderColumns := []string{column}
	if !opts.summaryOnly && len(opts.keyColumns) > 0 {
		orderColumns = opts.keyColumns
	}
	quotedOrder, err := quoteColumns(opts.dialect, orderColumns)
	if err != nil {
		return nil, categorizeRenderError(err)
	}
	query += " ORDER BY " + joinQuoted(quotedOrder)
	query += " LIMIT " + opts.dialect.Placeholder(len(pred.args)+1)

	args := append(append([]any(nil), pred.args...), limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, categorizeExecutionError(ctx, err)
	}

	var out []any
	for rows.Next() {
		var v any
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return nil, categorizeScanError(ctx, err)
		}
		out = append(out, v)
	}
	if err := finishRowsRead(ctx, rows); err != nil {
		return nil, err
	}
	return out, nil
}

func queryFailedKeys(
	ctx context.Context,
	db DB,
	table string,
	opts evalOptions,
	pred rowPredicate,
) ([]RowKey, error) {
	quoted, err := quoteColumns(opts.dialect, opts.keyColumns)
	if err != nil {
		return nil, categorizeRenderError(err)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", joinQuoted(quoted), table)
	if pred.where != "" {
		query += " WHERE " + pred.where
	}
	query += " ORDER BY " + joinQuoted(quoted)

	args := append([]any(nil), pred.args...)
	if opts.failedKeysCap > 0 {
		query += " LIMIT " + opts.dialect.Placeholder(len(args)+1)
		args = append(args, opts.failedKeysCap)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, categorizeExecutionError(ctx, err)
	}

	var keys []RowKey
	for rows.Next() {
		vals := make([]any, len(quoted))
		ptrs := make([]any, len(quoted))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			_ = rows.Close()
			return nil, categorizeScanError(ctx, err)
		}
		keys = append(keys, RowKey(vals))
	}
	if err := finishRowsRead(ctx, rows); err != nil {
		return nil, err
	}
	return keys, nil
}

func evalTableCount(
	ctx context.Context,
	db DB,
	table TableRef,
	opts evalOptions,
	kind ExpectationKind,
	label string,
	check func(int) bool,
	configured ResultFacts,
) (Result, error) {
	tbl, err := renderTable(opts.dialect, table)
	if err != nil {
		return Result{Kind: kind, Name: label, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}

	scopePred, err := composeRowPredicateWithScope(opts.scope, rowPredicate{}, opts.dialect)
	if err != nil {
		return Result{Kind: kind, Name: label, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}

	tableQuery, _ := countQuery(tbl, scopePred.where)
	tableArgs := append([]any(nil), scopePred.args...)

	count, err := queryCount(ctx, db, tbl, scopePred.where, scopePred.args)
	if err != nil {
		res := Result{Kind: kind, Name: label, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, tableQuery, tableArgs)
		return res, categorizeExecutionError(ctx, err)
	}

	name := fmt.Sprintf("%s: got %d", label, count)
	facts := configured
	countCopy := count
	facts.ObservedCount = &countCopy
	res := tableLevelResult(kind, "", name, check(count), facts)
	captureDiagnostics(&res, opts, tableQuery, tableArgs)
	return res, nil
}

func evalDistinctCount(
	ctx context.Context,
	db DB,
	table TableRef,
	opts evalOptions,
	kind ExpectationKind,
	column, label string,
	check func(int) bool,
	configured ResultFacts,
) (Result, error) {
	scopePred, err := composeRowPredicateWithScope(opts.scope, rowPredicate{}, opts.dialect)
	if err != nil {
		return Result{Kind: kind, Name: label, Column: column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}

	query, prepErr := distinctCountQuery(opts.dialect, table, column, scopePred.where)
	if prepErr != nil {
		return Result{Kind: kind, Name: label, Column: column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(prepErr)
	}

	args := append([]any(nil), scopePred.args...)
	count, err := queryScalarInt(ctx, db, query, args...)
	if err != nil {
		res := Result{Kind: kind, Name: label, Column: column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, query, args)
		return res, categorizeExecutionError(ctx, err)
	}

	name := fmt.Sprintf("%s: got %d", label, count)
	facts := configured
	countCopy := count
	facts.ObservedCount = &countCopy
	res := tableLevelResult(kind, column, name, check(count), facts)
	captureDiagnostics(&res, opts, query, args)
	return res, nil
}

func distinctCountQuery(d Dialect, table TableRef, column, where string) (string, error) {
	tbl, err := renderTable(d, table)
	if err != nil {
		return "", err
	}
	col, err := quoteIdent(d, column)
	if err != nil {
		return "", err
	}
	query := fmt.Sprintf("SELECT COUNT(DISTINCT %s) FROM %s", col, tbl)
	if where != "" {
		query += " WHERE " + where
	}
	return query, nil
}

func queryAggregateFloat(
	ctx context.Context,
	db DB,
	table TableRef,
	opts evalOptions,
	column, agg string,
) (float64, bool, string, error) {
	observed, ok, query, _, err := queryAggregateFloatWithArgs(ctx, db, table, opts, column, agg)
	return observed, ok, query, err
}

func queryAggregateFloatWithArgs(
	ctx context.Context,
	db DB,
	table TableRef,
	opts evalOptions,
	column, agg string,
) (float64, bool, string, []any, error) {
	tbl, err := renderTable(opts.dialect, table)
	if err != nil {
		return 0, false, "", nil, categorizeRenderError(err)
	}
	col, err := quoteIdent(opts.dialect, column)
	if err != nil {
		return 0, false, "", nil, categorizeRenderError(err)
	}

	scopePred, err := composeRowPredicateWithScope(opts.scope, rowPredicate{}, opts.dialect)
	if err != nil {
		return 0, false, "", nil, categorizeRenderError(err)
	}

	query := fmt.Sprintf("SELECT %s(%s) FROM %s", agg, col, tbl)
	if scopePred.where != "" {
		query += " WHERE " + scopePred.where
	}
	args := append([]any(nil), scopePred.args...)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, false, query, args, categorizeExecutionError(ctx, err)
	}

	var v sql.NullFloat64
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return 0, false, query, args, categorizeScanError(ctx, err)
		}
		if err := rows.Close(); err != nil {
			return 0, false, query, args, categorizeScanError(ctx, err)
		}
		return 0, false, query, args, categorizeScanError(ctx, sql.ErrNoRows)
	}
	if err := rows.Scan(&v); err != nil {
		_ = rows.Close()
		return 0, false, query, args, categorizeScanError(ctx, err)
	}
	if err := finishRowsRead(ctx, rows); err != nil {
		return 0, false, query, args, err
	}
	if !v.Valid {
		return 0, false, query, args, nil
	}
	return v.Float64, true, query, args, nil
}
