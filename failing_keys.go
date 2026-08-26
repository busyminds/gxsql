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

// rowStream owns row advancement and the single terminal transition shared by
// failure-key retrieval. A terminal transition releases rows and completes the
// privacy-safe observer event exactly once.
type rowStream struct {
	ctx       context.Context
	rows      *sql.Rows
	values    []any
	ptrs      []any
	err       error
	closed    bool
	exhausted bool
	start     time.Time
	observer  *observerState
	checkID   string
	checkKind ExpectationKind
	observed  bool
}

func newRowStream(
	ctx context.Context,
	rows *sql.Rows,
	width int,
	start time.Time,
	observer *observerState,
	checkID string,
	checkKind ExpectationKind,
) *rowStream {
	values := make([]any, width)
	ptrs := make([]any, width)
	for i := range values {
		ptrs[i] = &values[i]
	}
	return &rowStream{
		ctx:       ctx,
		rows:      rows,
		values:    values,
		ptrs:      ptrs,
		start:     start,
		observer:  observer,
		checkID:   checkID,
		checkKind: checkKind,
	}
}

func (s *rowStream) nextValues() ([]any, bool) {
	if s == nil || s.closed || s.err != nil || s.rows == nil {
		return nil, false
	}
	if s.ctx != nil && s.ctx.Err() != nil {
		s.err = categorizeExecutionError(s.ctx, s.ctx.Err())
		_ = s.rows.Close()
		s.rows = nil
		s.observeTerminal()
		return nil, false
	}
	if !s.rows.Next() {
		iterErr := s.rows.Err()
		closeErr := s.rows.Close()
		s.rows = nil
		switch {
		case iterErr != nil:
			s.err = categorizeScanError(s.ctx, iterErr)
		case closeErr != nil:
			s.err = categorizeScanError(s.ctx, closeErr)
		case s.ctx != nil && s.ctx.Err() != nil:
			s.err = categorizeExecutionError(s.ctx, s.ctx.Err())
		}
		if s.err == nil {
			s.exhausted = true
		}
		s.observeTerminal()
		return nil, false
	}
	if err := s.rows.Scan(s.ptrs...); err != nil {
		s.err = categorizeScanError(s.ctx, err)
		_ = s.rows.Close()
		s.rows = nil
		s.observeTerminal()
		return nil, false
	}
	return s.values, true
}

func (s *rowStream) close() error {
	if s == nil {
		return nil
	}
	if s.closed {
		return s.err
	}
	s.closed = true
	if s.rows != nil {
		closeErr := s.rows.Close()
		s.rows = nil
		if s.err == nil && closeErr != nil {
			s.err = categorizeScanError(s.ctx, closeErr)
		}
	}
	if s.err == nil && !s.exhausted && s.ctx != nil && s.ctx.Err() != nil {
		s.err = categorizeExecutionError(s.ctx, s.ctx.Err())
	}
	s.observeTerminal()
	return s.err
}

func (s *rowStream) observeTerminal() {
	if s == nil || s.observed || s.observer == nil {
		return
	}
	s.observed = true
	start := s.start
	if start.IsZero() {
		start = time.Now()
	}
	if obsErr := s.observer.observe(start, s.checkID, s.checkKind, QueryCategoryFailingKeys, s.err); obsErr != nil && s.err == nil {
		s.err = obsErr
	}
}

// FailureKeyIterator streams complete failing row keys for one selected result.
// Keys are yielded in deterministic ORDER BY key-column order. Call [Close]
// after iteration, including when the caller stops before [Next] returns false.
// Mutating a [RowKey] return value does not affect later keys.
type FailureKeyIterator struct {
	stream  *rowStream
	current RowKey
}

// Next advances to the next failing key. It returns false when iteration is
// exhausted, cancelled, closed, or an error occurs. Inspect [Err] after false.
func (it *FailureKeyIterator) Next() bool {
	if it == nil || it.stream == nil {
		return false
	}
	values, ok := it.stream.nextValues()
	if !ok {
		return false
	}
	if cap(it.current) < len(values) {
		it.current = make(RowKey, len(values))
	} else {
		it.current = it.current[:len(values)]
	}
	copy(it.current, values)
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
	if it == nil || it.stream == nil {
		return nil
	}
	return it.stream.err
}

// Close releases the underlying rows. It is safe to call multiple times.
// Close after a successful drain is a no-op success; Close after cancellation
// may surface the context error.
func (it *FailureKeyIterator) Close() error {
	if it == nil || it.stream == nil {
		return nil
	}
	return it.stream.close()
}

// ForResultID selects the report result with the given stable [Result.ID].
// Segmented reports repeat IDs across segments, so ID selection is ambiguous
// there; use [ForResultIndex] to pick a segment-major result slot.
func ForResultID(id string) Option {
	return func(cfg *validateConfig) {
		cfg.failingKeysID = id
		cfg.hasFailingKeysID = true
	}
}

// ForResultIndex selects the report result at the given index into
// [Report.Results]. Unsegmented reports use expectation declaration order;
// segmented reports use segment-major then expectation order.
func ForResultIndex(index int) Option {
	return func(cfg *validateConfig) {
		cfg.failingKeysIndex = index
		cfg.hasFailingKeysIndex = true
	}
}

// ForKind selects the sole report result with the given [ExpectationKind].
// Multiple matches are rejected as ambiguous. Segmented reports repeat kinds
// across segments, so kind selection is ambiguous there; use [ForResultIndex]
// to pick a segment-major result slot.
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
// or [ForKind] is required and must be unambiguous. Segmented reports repeat
// expectation IDs and kinds per segment, so [ForResultID] and [ForKind] are
// typically ambiguous; [ForResultIndex] selects the segment-major result slot.
// Unsupported expectations return [CategoryUnsupported].
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
		stream: newRowStream(
			ctx,
			rows,
			len(keyColumns),
			start,
			observer,
			res.ID,
			res.Kind,
		),
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
