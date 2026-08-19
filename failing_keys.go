package gxsql

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// failureKeyPlan is the internal read-only retrieval plan attached to supported
// results so [FailingKeys] can stream complete failing identity without retaining
// the full key set on [Report] / [Result].
type failureKeyPlan struct {
	fromSQL    string
	tableAlias string
	target     TableRef
	scopeID    string
	dialect    Dialect
	failPred   rowPredicate
}

func attachFailureKeyPlan(res *Result, target TableRef, fromSQL, tableAlias string, failPred rowPredicate, scope *trustedScope, dialect Dialect) {
	if res == nil {
		return
	}
	scopeID := ""
	if scope != nil {
		scopeID = scope.identity
	}
	res.failureKeys = &failureKeyPlan{
		fromSQL:    fromSQL,
		tableAlias: tableAlias,
		target:     target,
		scopeID:    scopeID,
		dialect:    dialect,
		failPred: rowPredicate{
			where: failPred.where,
			args:  copyScopeValues(failPred.args),
		},
	}
}

// FailureKeyIterator streams complete failing row keys for one selected result.
// Keys are yielded in deterministic ORDER BY key-column order. Call [Close]
// after iteration, including when the caller stops before [Next] returns false.
// Mutating a [RowKey] return value does not affect later keys.
type FailureKeyIterator struct {
	ctx       context.Context
	rows      *sql.Rows
	width     int
	current   RowKey
	err       error
	closed    bool
	exhausted bool
	started   bool
	start     time.Time
	observer  *observerState
	checkID   string
	checkKind ExpectationKind
	observed  bool
}

// Next advances to the next failing key. It returns false when iteration is
// exhausted, cancelled, closed, or an error occurs. Inspect [Err] after false.
func (it *FailureKeyIterator) Next() bool {
	if it == nil || it.closed || it.err != nil || it.rows == nil {
		return false
	}
	if !it.started {
		it.started = true
		it.start = time.Now()
	}
	if it.ctx != nil && it.ctx.Err() != nil {
		it.err = categorizeExecutionError(it.ctx, it.ctx.Err())
		_ = it.rows.Close()
		it.rows = nil
		it.observeTerminal()
		return false
	}
	if !it.rows.Next() {
		iterErr := it.rows.Err()
		closeErr := it.rows.Close()
		it.rows = nil
		switch {
		case iterErr != nil:
			it.err = categorizeScanError(it.ctx, iterErr)
		case closeErr != nil:
			it.err = categorizeScanError(it.ctx, closeErr)
		case it.ctx != nil && it.ctx.Err() != nil:
			it.err = categorizeExecutionError(it.ctx, it.ctx.Err())
		}
		if it.err == nil {
			it.exhausted = true
		}
		it.observeTerminal()
		return false
	}
	vals := make([]any, it.width)
	ptrs := make([]any, it.width)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := it.rows.Scan(ptrs...); err != nil {
		it.err = categorizeScanError(it.ctx, err)
		_ = it.rows.Close()
		it.rows = nil
		it.observeTerminal()
		return false
	}
	it.current = RowKey(vals)
	return true
}

// Key returns a copy of the current failing key. It is only valid after a
// successful [Next] call.
func (it *FailureKeyIterator) Key() RowKey {
	if it == nil || it.current == nil {
		return nil
	}
	out := make(RowKey, len(it.current))
	copy(out, it.current)
	return out
}

// Err returns the first iteration, scan, database, or context error.
func (it *FailureKeyIterator) Err() error {
	if it == nil {
		return nil
	}
	return it.err
}

// Close releases the underlying rows. It is safe to call multiple times.
// Close after a successful drain is a no-op success; Close after cancellation
// may surface the context error.
func (it *FailureKeyIterator) Close() error {
	if it == nil {
		return nil
	}
	if it.closed {
		return it.err
	}
	it.closed = true
	if it.rows != nil {
		closeErr := it.rows.Close()
		it.rows = nil
		if it.err == nil && closeErr != nil {
			it.err = categorizeScanError(it.ctx, closeErr)
		}
	}
	if it.err == nil && !it.exhausted && it.ctx != nil && it.ctx.Err() != nil {
		it.err = categorizeExecutionError(it.ctx, it.ctx.Err())
	}
	it.observeTerminal()
	return it.err
}

func (it *FailureKeyIterator) finishObserve(err error) error {
	if it == nil || it.observed || it.observer == nil {
		return nil
	}
	it.observed = true
	start := it.start
	if start.IsZero() {
		start = time.Now()
	}
	return it.observer.observe(start, it.checkID, it.checkKind, QueryCategoryFailingKeys, err)
}

// observeTerminal records the observer event for a terminal iterator path.
// Existing context/database/scan errors take precedence over observer errors.
func (it *FailureKeyIterator) observeTerminal() {
	if obsErr := it.finishObserve(it.err); obsErr != nil && it.err == nil {
		it.err = obsErr
	}
}

// ForResultID selects the report result with the given stable [Result.ID].
func ForResultID(id string) Option {
	return func(cfg *validateConfig) {
		cfg.failingKeysID = id
		cfg.hasFailingKeysID = true
	}
}

// ForResultIndex selects the report result at the given declaration-order index.
func ForResultIndex(index int) Option {
	return func(cfg *validateConfig) {
		cfg.failingKeysIndex = index
		cfg.hasFailingKeysIndex = true
	}
}

// ForKind selects the sole report result with the given [ExpectationKind].
// Multiple matches are rejected as ambiguous.
func ForKind(kind ExpectationKind) Option {
	return func(cfg *validateConfig) {
		cfg.failingKeysKind = kind
		cfg.hasFailingKeysKind = true
	}
}

// FailingKeys returns a read-only iterator over the complete failing key set for
// one explicitly selected result. It re-runs the failure predicate as a
// deterministic SELECT ... ORDER BY key columns and never retains the full set
// on [Report]. [WithKey] (or retained report key columns from ValidateTable
// [WithKey]) is required. Target binding via [ForResultID], [ForResultIndex],
// or [ForKind] is required and must be unambiguous. Unsupported expectations
// return [CategoryUnsupported].
func FailingKeys(
	ctx context.Context,
	db DB,
	table TableRef,
	report Report,
	opts ...Option,
) (*FailureKeyIterator, error) {
	if ctx == nil {
		return nil, newConfigError(fmt.Errorf("context is required"))
	}
	if db == nil {
		return nil, newConfigError(fmt.Errorf("database is required"))
	}
	cfg := validateConfig{
		dialect: Postgres(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.dialect == nil {
		return nil, newConfigError(fmt.Errorf("dialect is required"))
	}

	keyColumns := append([]string(nil), cfg.keyColumns...)
	if len(keyColumns) == 0 {
		keyColumns = append([]string(nil), report.keyColumns...)
	}
	if len(keyColumns) == 0 {
		return nil, newConfigError(fmt.Errorf("failing keys require WithKey columns"))
	}
	for _, col := range keyColumns {
		if err := validateIdent(col); err != nil {
			return nil, newConfigError(err)
		}
	}

	res, err := selectFailingKeysResult(report, &cfg)
	if err != nil {
		return nil, err
	}
	if res.failureKeys == nil {
		return nil, &CategorizedError{
			Category: CategoryUnsupported,
			Err:      fmt.Errorf("failing keys unsupported for result kind %s", res.Kind),
		}
	}

	plan := res.failureKeys
	if plan.target != table {
		return nil, newConfigError(fmt.Errorf("retrieval table does not match validated target"))
	}
	if err := validateFailingKeysScope(cfg, plan.scopeID); err != nil {
		return nil, err
	}
	if plan.dialect == nil {
		return nil, newConfigError(fmt.Errorf("validated result has no dialect"))
	}

	quoted, err := quoteColumns(plan.dialect, keyColumns)
	if err != nil {
		return nil, categorizeRenderError(err)
	}
	for i := range quoted {
		quoted[i] = qualifySQLIdent(plan.tableAlias, quoted[i])
	}

	query := fmt.Sprintf("SELECT %s FROM %s", joinQuoted(quoted), plan.fromSQL)
	if plan.failPred.where != "" {
		query += " WHERE " + plan.failPred.where
	}
	query += " ORDER BY " + joinQuoted(quoted)
	args := append([]any(nil), plan.failPred.args...)

	observer := &observerState{observer: cfg.observer}
	start := time.Now()
	rows, qerr := db.QueryContext(ctx, query, args...)
	if qerr != nil {
		err := categorizeExecutionError(ctx, qerr)
		if obsErr := observer.observe(start, res.ID, res.Kind, QueryCategoryFailingKeys, err); obsErr != nil {
			return nil, obsErr
		}
		return nil, err
	}

	return &FailureKeyIterator{
		ctx:       ctx,
		rows:      rows,
		width:     len(keyColumns),
		start:     start,
		started:   true,
		observer:  observer,
		checkID:   res.ID,
		checkKind: res.Kind,
	}, nil
}

// validateFailingKeysScope validates an optional WithScope against the
// immutable validate-time scope identity stored on the plan.
func validateFailingKeysScope(cfg validateConfig, planScopeID string) error {
	if !cfg.hasScope {
		return nil
	}
	scope, err := validateScope(cfg.scope)
	if err != nil {
		return err
	}
	if planScopeID == "" || scope.identity != planScopeID {
		return newConfigError(fmt.Errorf(
			"scope identity %q does not match validated scope %q",
			scope.identity, planScopeID,
		))
	}
	return nil
}

func selectFailingKeysResult(report Report, cfg *validateConfig) (Result, error) {
	bindings := 0
	if cfg.hasFailingKeysID {
		bindings++
	}
	if cfg.hasFailingKeysIndex {
		bindings++
	}
	if cfg.hasFailingKeysKind {
		bindings++
	}
	if bindings == 0 {
		return Result{}, newConfigError(fmt.Errorf("failing keys target binding is required"))
	}
	if bindings > 1 {
		return Result{}, newConfigError(fmt.Errorf("failing keys target binding is ambiguous"))
	}

	switch {
	case cfg.hasFailingKeysID:
		id := cfg.failingKeysID
		var found *Result
		for i := range report.Results {
			if report.Results[i].ID == id {
				if found != nil {
					return Result{}, newConfigError(fmt.Errorf("failing keys result id %q is ambiguous", id))
				}
				res := report.Results[i]
				found = &res
			}
		}
		if found == nil {
			return Result{}, newConfigError(fmt.Errorf("failing keys result id %q not found", id))
		}
		return *found, nil

	case cfg.hasFailingKeysIndex:
		idx := cfg.failingKeysIndex
		if idx < 0 || idx >= len(report.Results) {
			return Result{}, newConfigError(fmt.Errorf("failing keys result index %d is out of range", idx))
		}
		return report.Results[idx], nil

	default:
		kind := cfg.failingKeysKind
		var found *Result
		for i := range report.Results {
			if report.Results[i].Kind == kind {
				if found != nil {
					return Result{}, newConfigError(fmt.Errorf("failing keys result kind %s is ambiguous", kind))
				}
				res := report.Results[i]
				found = &res
			}
		}
		if found == nil {
			return Result{}, newConfigError(fmt.Errorf("failing keys result kind %s not found", kind))
		}
		return *found, nil
	}
}
