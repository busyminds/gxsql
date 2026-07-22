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

	totalPred, err := composeRowPredicateWithScope(opts.scope, rowPredicate{}, opts.dialect)
	if err != nil {
		return Result{Kind: kind, Name: displayName, Column: column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}

	failQuery, failArgs := failedCountDiagnostics(tbl, failPred)

	total, err := queryCount(ctx, db, tbl, totalPred.where, totalPred.args)
	if err != nil {
		res := Result{Kind: kind, Name: displayName, Column: column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, categorizeExecutionError(ctx, err)
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

	tableQuery, tableArgs := countQuery(tbl, "")

	count, err := queryCount(ctx, db, tbl, "", nil)
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
	query, prepErr := distinctCountQuery(opts.dialect, table, column)
	if prepErr != nil {
		return Result{Kind: kind, Name: label, Column: column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(prepErr)
	}

	count, err := queryScalarInt(ctx, db, query)
	if err != nil {
		res := Result{Kind: kind, Name: label, Column: column, RowDenominator: RowDenominatorUnavailable}
		captureDiagnostics(&res, opts, query, nil)
		return res, categorizeExecutionError(ctx, err)
	}

	name := fmt.Sprintf("%s: got %d", label, count)
	facts := configured
	countCopy := count
	facts.ObservedCount = &countCopy
	res := tableLevelResult(kind, column, name, check(count), facts)
	captureDiagnostics(&res, opts, query, nil)
	return res, nil
}

func distinctCountQuery(d Dialect, table TableRef, column string) (string, error) {
	tbl, err := renderTable(d, table)
	if err != nil {
		return "", err
	}
	col, err := quoteIdent(d, column)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("SELECT COUNT(DISTINCT %s) FROM %s", col, tbl), nil
}

func queryAggregateFloat(
	ctx context.Context,
	db DB,
	table TableRef,
	opts evalOptions,
	column, agg string,
) (float64, bool, string, error) {
	tbl, err := renderTable(opts.dialect, table)
	if err != nil {
		return 0, false, "", categorizeRenderError(err)
	}
	col, err := quoteIdent(opts.dialect, column)
	if err != nil {
		return 0, false, "", categorizeRenderError(err)
	}

	query := fmt.Sprintf("SELECT %s(%s) FROM %s", agg, col, tbl)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return 0, false, query, categorizeExecutionError(ctx, err)
	}

	var v sql.NullFloat64
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return 0, false, query, categorizeScanError(ctx, err)
		}
		if err := rows.Close(); err != nil {
			return 0, false, query, categorizeScanError(ctx, err)
		}
		return 0, false, query, categorizeScanError(ctx, sql.ErrNoRows)
	}
	if err := rows.Scan(&v); err != nil {
		_ = rows.Close()
		return 0, false, query, categorizeScanError(ctx, err)
	}
	if err := finishRowsRead(ctx, rows); err != nil {
		return 0, false, query, err
	}
	if !v.Valid {
		return 0, false, query, nil
	}
	return v.Float64, true, query, nil
}
