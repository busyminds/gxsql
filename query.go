package gxsql

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
	return queryColumnSamplesAs(ctx, db, table, "", column, pred, opts, limit)
}

// queryColumnSamplesAs loads capped sample values from failing rows. When
// tableAlias is non-empty, SELECT and ORDER BY columns are qualified with that
// alias so aliased FROM clauses remain unambiguous.
func queryColumnSamplesAs(
	ctx context.Context,
	db DB,
	table, tableAlias, column string,
	pred rowPredicate,
	opts evalOptions,
	limit int,
) ([]any, error) {
	quotedColumn, err := quoteIdent(opts.dialect, column)
	if err != nil {
		return nil, categorizeRenderError(err)
	}
	quotedColumn = qualifySQLIdent(tableAlias, quotedColumn)

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
	for i := range quotedOrder {
		quotedOrder[i] = qualifySQLIdent(tableAlias, quotedOrder[i])
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
	return queryFailedKeysAs(ctx, db, table, "", opts, pred)
}

// queryFailedKeysAs loads capped failed keys from failing rows. When tableAlias
// is non-empty, SELECT and ORDER BY key columns are qualified with that alias.
func queryFailedKeysAs(
	ctx context.Context,
	db DB,
	table, tableAlias string,
	opts evalOptions,
	pred rowPredicate,
) ([]RowKey, error) {
	quoted, err := quoteColumns(opts.dialect, opts.keyColumns)
	if err != nil {
		return nil, categorizeRenderError(err)
	}
	for i := range quoted {
		quoted[i] = qualifySQLIdent(tableAlias, quoted[i])
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

func qualifySQLIdent(alias, quoted string) string {
	if alias == "" {
		return quoted
	}
	return alias + "." + quoted
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

// queryAggregateTimeWithArgs runs SELECT <agg>(column) over the scoped table and
// scans a nullable timestamp. ok is false when the aggregate is SQL NULL
// (empty population or all-NULL values). The returned query and args are the
// aggregate statement only; callers capture them under CaptureQueryDiagnostics.
func queryAggregateTimeWithArgs(
	ctx context.Context,
	db DB,
	table TableRef,
	opts evalOptions,
	column, agg string,
) (time.Time, bool, string, []any, error) {
	tbl, err := renderTable(opts.dialect, table)
	if err != nil {
		return time.Time{}, false, "", nil, categorizeRenderError(err)
	}
	col, err := quoteIdent(opts.dialect, column)
	if err != nil {
		return time.Time{}, false, "", nil, categorizeRenderError(err)
	}

	scopePred, err := composeRowPredicateWithScope(opts.scope, rowPredicate{}, opts.dialect)
	if err != nil {
		return time.Time{}, false, "", nil, categorizeRenderError(err)
	}

	query := fmt.Sprintf("SELECT %s(%s) FROM %s", agg, col, tbl)
	if scopePred.where != "" {
		query += " WHERE " + scopePred.where
	}
	args := append([]any(nil), scopePred.args...)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return time.Time{}, false, query, args, categorizeExecutionError(ctx, err)
	}

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return time.Time{}, false, query, args, categorizeScanError(ctx, err)
		}
		if err := rows.Close(); err != nil {
			return time.Time{}, false, query, args, categorizeScanError(ctx, err)
		}
		return time.Time{}, false, query, args, categorizeScanError(ctx, sql.ErrNoRows)
	}
	var raw any
	if err := rows.Scan(&raw); err != nil {
		_ = rows.Close()
		return time.Time{}, false, query, args, categorizeScanError(ctx, err)
	}
	if err := finishRowsRead(ctx, rows); err != nil {
		return time.Time{}, false, query, args, err
	}
	observed, ok, err := coerceScannedTime(raw)
	if err != nil {
		return time.Time{}, false, query, args, categorizeScanError(ctx, err)
	}
	if !ok {
		return time.Time{}, false, query, args, nil
	}
	return observed, true, query, args, nil
}

func coerceScannedTime(raw any) (time.Time, bool, error) {
	switch v := raw.(type) {
	case nil:
		return time.Time{}, false, nil
	case time.Time:
		return v, true, nil
	case *time.Time:
		if v == nil {
			return time.Time{}, false, nil
		}
		return *v, true, nil
	case []byte:
		if len(v) == 0 {
			return time.Time{}, false, nil
		}
		return parseScannedTimeString(string(v))
	case string:
		if v == "" {
			return time.Time{}, false, nil
		}
		return parseScannedTimeString(v)
	default:
		return time.Time{}, false, fmt.Errorf("unsupported timestamp scan type %T", raw)
	}
}

func parseScannedTimeString(s string) (time.Time, bool, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999-07",
		"2006-01-02 15:04:05.999999999 +0000 UTC",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05Z07:00",
	}
	var err error
	for _, layout := range layouts {
		var ts time.Time
		ts, err = time.Parse(layout, s)
		if err == nil {
			return ts, true, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("parse timestamp %q: %w", s, err)
}

// queryCustomCountScalar executes a rendered trusted-count query and requires
// exactly one row with one signed integer column representable as a non-negative int.
func queryCustomCountScalar(ctx context.Context, db DB, query string, args ...any) (int, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, categorizeExecutionError(ctx, err)
	}

	cols, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		return 0, customCountScanError(err)
	}
	if len(cols) != 1 {
		_ = rows.Close()
		return 0, customCountScanError(errCustomCountWrongColumnCount)
	}

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return 0, customCountScanError(err)
		}
		if err := rows.Close(); err != nil {
			return 0, customCountScanError(err)
		}
		return 0, customCountScanError(errCustomCountNoRows)
	}

	var raw any
	if err := rows.Scan(&raw); err != nil {
		_ = rows.Close()
		return 0, customCountScanError(err)
	}
	if rows.Next() {
		_ = rows.Close()
		return 0, customCountScanError(errCustomCountMultipleRows)
	}
	if err := finishRowsRead(ctx, rows); err != nil {
		return 0, customCountScanError(err)
	}
	count, err := customCountInt(raw)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func customCountInt(value any) (int, error) {
	var n int64
	switch v := value.(type) {
	case int:
		n = int64(v)
	case int8:
		n = int64(v)
	case int16:
		n = int64(v)
	case int32:
		n = int64(v)
	case int64:
		n = v
	default:
		if value == nil {
			return 0, customCountScanError(errCustomCountNull)
		}
		return 0, customCountScanError(errCustomCountNonInteger)
	}
	if n < 0 {
		return 0, customCountScanError(errCustomCountNegative)
	}
	count := int(n)
	if int64(count) != n {
		return 0, customCountScanError(errCustomCountOverflow)
	}
	return count, nil
}

func evalCustomCount(
	ctx context.Context,
	db DB,
	table TableRef,
	opts evalOptions,
	displayName, template string,
	customArgs []any,
) (Result, error) {
	query, args, err := renderTrustedCount(opts.dialect, table, opts.scope, template, customArgs)
	if err != nil {
		res := Result{
			Kind:           KindCustom,
			Name:           displayName,
			RowDenominator: RowDenominatorUnavailable,
			shape:          resultShapeCustomCount,
		}
		return res, err
	}

	count, err := queryCustomCountScalar(ctx, db, query, args...)
	if err != nil {
		res := Result{
			Kind:           KindCustom,
			Name:           displayName,
			RowDenominator: RowDenominatorUnavailable,
			shape:          resultShapeCustomCount,
		}
		captureDiagnostics(&res, opts, query, args)
		return res, customCountPrivacyError(ctx, err)
	}

	res, err := customCountResult(displayName, count)
	if err != nil {
		return res, err
	}
	captureDiagnostics(&res, opts, query, args)
	return res, nil
}
