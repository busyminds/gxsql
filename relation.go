package gxsql

import (
	"context"
	"fmt"
	"strings"
)

const (
	referenceLocalAlias  = "__gx_local"
	referenceParentAlias = "__gx_parent"
)

type referenceExpectation struct {
	localColumns  []string
	parent        TableRef
	parentColumns []string
}

func (e referenceExpectation) Name() string {
	parent := e.parent.Name
	if e.parent.Schema != "" {
		parent = e.parent.Schema + "." + e.parent.Name
	}
	return strings.Join(e.localColumns, ", ") + " references " + parent + " (" + strings.Join(e.parentColumns, ", ") + ")"
}

func (e referenceExpectation) expectationKind() ExpectationKind { return KindReference }

func (e referenceExpectation) preflight() error {
	if err := validateReferenceMapping(e.localColumns, e.parent, e.parentColumns); err != nil {
		return newConfigError(err)
	}
	return nil
}

func (e referenceExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	facts := ResultFacts{
		Reference: &ReferenceFacts{
			LocalColumns:  append([]string(nil), e.localColumns...),
			Parent:        e.parent,
			ParentColumns: append([]string(nil), e.parentColumns...),
		},
	}
	base := Result{
		Kind:           KindReference,
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
	localAlias, err := quoteIdent(opts.dialect, referenceLocalAlias)
	if err != nil {
		return base, categorizeRenderError(err)
	}
	parentAlias, err := quoteIdent(opts.dialect, referenceParentAlias)
	if err != nil {
		return base, categorizeRenderError(err)
	}
	localQuoted, err := quoteColumns(opts.dialect, e.localColumns)
	if err != nil {
		return base, categorizeRenderError(err)
	}
	parentTbl, err := renderTable(opts.dialect, e.parent)
	if err != nil {
		return base, categorizeRenderError(err)
	}
	parentQuoted, err := quoteColumns(opts.dialect, e.parentColumns)
	if err != nil {
		return base, categorizeRenderError(err)
	}

	outerFrom := tbl + " AS " + localAlias
	orphanPred, err := referenceOrphanPredicate(localAlias, localQuoted, parentTbl, parentAlias, parentQuoted)
	if err != nil {
		return base, categorizeRenderError(err)
	}
	failPred := orphanPred
	if opts.scope != nil {
		scopePred, err := opts.scope.render(opts.dialect)
		if err != nil {
			return base, categorizeRenderError(err)
		}
		scopePred = rewriteScopeTargetForAlias(scopePred, table, tbl, localAlias)
		failPred = withWhere(
			"("+scopePred.where+") AND ("+orphanPred.where+")",
			append(scopePred.args, orphanPred.args...),
		)
	}
	failQuery, failArgs := failedCountDiagnostics(outerFrom, failPred)

	total, err := resolveScopedTotal(ctx, db, table, opts)
	if err != nil {
		res := base
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, err
	}

	failed, err := queryCount(
		ctx, db, opts, QueryCategoryFailureCount, outerFrom, failPred.where, failPred.args,
	)
	if err != nil {
		res := base
		captureDiagnostics(&res, opts, failQuery, failArgs)
		return res, categorizeExecutionError(ctx, err)
	}

	res := perRowResult(KindReference, "", e.Name(), total, failed, facts)
	captureDiagnostics(&res, opts, failQuery, failArgs)
	if failed == 0 {
		return res, nil
	}

	if opts.sampleCap > 0 {
		samples, err := queryColumnSamplesAs(ctx, db, outerFrom, localAlias, e.localColumns[0], failPred, opts, opts.sampleCap)
		if err != nil {
			return res, categorizeExecutionError(ctx, err)
		}
		res.SampleValues = samples
	}

	if !opts.summaryOnly && len(opts.keyColumns) > 0 {
		keys, err := queryFailedKeysAs(ctx, db, outerFrom, localAlias, opts, failPred)
		if err != nil {
			return res, categorizeExecutionError(ctx, err)
		}
		res.FailedKeys = keys
	}
	return res, nil
}

func validateReferenceMapping(local []string, parent TableRef, parentColumns []string) error {
	if len(local) == 0 {
		return fmt.Errorf("gxsql: reference requires at least one local column")
	}
	if len(parentColumns) == 0 {
		return fmt.Errorf("gxsql: reference requires at least one parent column")
	}
	if len(local) != len(parentColumns) {
		return fmt.Errorf("gxsql: reference arity mismatch: %d local columns vs %d parent columns", len(local), len(parentColumns))
	}
	if err := validateIdent(parent.Name); err != nil {
		return err
	}
	if parent.Schema != "" {
		if err := validateIdent(parent.Schema); err != nil {
			return err
		}
	}
	if err := validateDistinctIdents(local, "local reference column"); err != nil {
		return err
	}
	if err := validateDistinctIdents(parentColumns, "parent reference column"); err != nil {
		return err
	}
	return nil
}

func validateDistinctIdents(names []string, label string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if err := validateIdent(name); err != nil {
			return err
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("gxsql: duplicate %s %q", label, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// referenceOrphanPredicate is true for complete local tuples with no matching
// unscoped parent row. Scope must be composed onto the local side only; the
// parent subquery is intentionally unscoped.
func referenceOrphanPredicate(
	localAlias string,
	localQuoted []string,
	parentTable string,
	parentAlias string,
	parentQuoted []string,
) (rowPredicate, error) {
	if len(localQuoted) == 0 || len(localQuoted) != len(parentQuoted) {
		return rowPredicate{}, fmt.Errorf("gxsql: reference predicate arity mismatch")
	}
	parts := make([]string, 0, len(localQuoted)+1)
	for _, col := range localQuoted {
		parts = append(parts, localAlias+"."+col+" IS NOT NULL")
	}
	eqs := make([]string, len(localQuoted))
	for i := range localQuoted {
		eqs[i] = parentAlias + "." + parentQuoted[i] + " = " + localAlias + "." + localQuoted[i]
	}
	parts = append(parts, fmt.Sprintf(
		"NOT EXISTS (SELECT 1 FROM %s AS %s WHERE %s)",
		parentTable, parentAlias, strings.Join(eqs, " AND "),
	))
	return withWhere(strings.Join(parts, " AND "), nil), nil
}

func rewriteScopeTargetForAlias(pred rowPredicate, table TableRef, renderedTarget, localAlias string) rowPredicate {
	where := pred.where
	aliasPrefix := localAlias + "."

	targetPrefixes := []string{}
	if renderedTarget != "" {
		targetPrefixes = append(targetPrefixes, renderedTarget+".")
	}
	if table.Schema != "" {
		targetPrefixes = append(targetPrefixes, table.Schema+"."+table.Name+".")
	}
	targetPrefixes = append(targetPrefixes, table.Name+".")
	for _, prefix := range targetPrefixes {
		where = strings.ReplaceAll(where, prefix, aliasPrefix)
	}
	return withWhere(where, append([]any(nil), pred.args...))
}

// dialectSupportsRelationalKeys reports whether d can render relational key
// integrity SQL. Built-in dialects implement the unexported marker method;
// custom Dialect values remain compatible with the public Dialect interface and
// must opt in by implementing the same marker if they support these checks.
func dialectSupportsRelationalKeys(d Dialect) bool {
	type relationalKeys interface{ supportsRelationalKeys() }
	_, ok := d.(relationalKeys)
	return ok
}

func requiresRelationalDialect(exp Expectation) bool {
	switch unwrapExpectation(exp).(type) {
	case referenceExpectation, compositeUniqueExpectation:
		return true
	default:
		return false
	}
}

func unsupportedRelationalDialectError() error {
	return &CategorizedError{
		Category: CategoryUnsupported,
		Err:      fmt.Errorf("gxsql: dialect does not support relational key integrity"),
	}
}
