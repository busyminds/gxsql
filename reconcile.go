package gxsql

import (
	"context"
	"fmt"
)

const reconcileRelationshipEqual = "equal"

// ReconcileCountsBuilder builds suite-bound COUNT(*) reconciliation
// expectations. Start with [ReconcileCounts]. The table passed to
// [Suite.ValidateTable] is always the left side; secondary is explicit.
// Suite [WithScope] applies only to the left COUNT(*). Narrow the secondary
// population with [ReconcileCountsBuilder.WithSecondaryFilter].
type ReconcileCountsBuilder struct {
	secondary          TableRef
	secondaryFilter    SecondaryFilter
	hasSecondaryFilter bool
}

// ReconcileCounts returns a builder that reconciles COUNT(*) on the
// ValidateTable target against secondary. Results use
// [KindReconcileCountsEqual], set [RowDenominatorUnavailable], publish
// [ResultFacts.Reconcile], and do not collect samples or failed keys.
// Table-level reconciliation is ineligible for failure tolerance.
func ReconcileCounts(secondary TableRef) ReconcileCountsBuilder {
	return ReconcileCountsBuilder{secondary: secondary}
}

// WithSecondaryFilter returns a copy that applies filter only to the secondary
// COUNT(*). Suite [WithScope] remains left-only. Filter identity is published
// on [ReconcileFacts.SecondaryFilterID]; predicate text and args are never
// exported by default.
func (b ReconcileCountsBuilder) WithSecondaryFilter(filter SecondaryFilter) ReconcileCountsBuilder {
	b.secondaryFilter = SecondaryFilter{
		identity:  filter.identity,
		predicate: filter.predicate,
		values:    copyScopeValues(filter.values),
	}
	b.hasSecondaryFilter = true
	return b
}

// Equal returns a table-level expectation that left and right COUNT(*) values
// are equal. Equality yields FailedCount 0; inequality yields FailedCount 1.
func (b ReconcileCountsBuilder) Equal() Expectation {
	return reconcileCountsExpectation(b)
}

type reconcileCountsExpectation struct {
	secondary          TableRef
	secondaryFilter    SecondaryFilter
	hasSecondaryFilter bool
}

func (e reconcileCountsExpectation) Name() string {
	return "reconcile counts equal " + tableRefDisplay(e.secondary)
}

func (e reconcileCountsExpectation) expectationKind() ExpectationKind {
	return KindReconcileCountsEqual
}

func (e reconcileCountsExpectation) preflight() error {
	if err := validateIdent(e.secondary.Name); err != nil {
		return newConfigError(err)
	}
	if e.secondary.Schema != "" {
		if err := validateIdent(e.secondary.Schema); err != nil {
			return newConfigError(err)
		}
	}
	if e.hasSecondaryFilter {
		if _, err := validateSecondaryFilter(e.secondaryFilter); err != nil {
			return err
		}
	}
	return nil
}

func (e reconcileCountsExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	facts := &ReconcileFacts{
		Left:         table,
		Right:        e.secondary,
		Relationship: reconcileRelationshipEqual,
	}
	if opts.scope != nil {
		facts.LeftScopeID = opts.scope.identity
	}

	var validatedFilter SecondaryFilter
	if e.hasSecondaryFilter {
		filter, err := validateSecondaryFilter(e.secondaryFilter)
		if err != nil {
			return e.baseResult(facts), err
		}
		validatedFilter = filter
		facts.SecondaryFilterID = filter.identity
	}

	leftTbl, err := renderTable(opts.dialect, table)
	if err != nil {
		return e.baseResult(facts), categorizeRenderError(err)
	}
	leftPred, err := composeRowPredicateWithScope(opts.scope, rowPredicate{}, opts.dialect)
	if err != nil {
		return e.baseResult(facts), categorizeRenderError(err)
	}
	leftQuery, _ := countQuery(leftTbl, leftPred.where)
	leftArgs := append([]any(nil), leftPred.args...)

	leftCount, err := queryCount(
		ctx, db, opts, QueryCategoryTotalCount, leftTbl, leftPred.where, leftPred.args,
	)
	if err != nil {
		res := e.baseResult(facts)
		captureDiagnostics(&res, opts, leftQuery, leftArgs)
		return res, categorizeExecutionError(ctx, err)
	}
	facts.ObservedLeftCount = intFact(leftCount)

	rightTbl, err := renderTable(opts.dialect, e.secondary)
	if err != nil {
		res := e.baseResult(facts)
		captureDiagnostics(&res, opts, leftQuery, leftArgs)
		return res, categorizeRenderError(err)
	}
	var rightPred rowPredicate
	if e.hasSecondaryFilter {
		rightPred, err = validatedFilter.render(opts.dialect)
		if err != nil {
			res := e.baseResult(facts)
			captureDiagnostics(&res, opts, leftQuery, leftArgs)
			return res, categorizeRenderError(err)
		}
	}
	rightQuery, _ := countQuery(rightTbl, rightPred.where)
	rightArgs := append([]any(nil), rightPred.args...)

	rightCount, err := queryCount(
		ctx, db, opts, QueryCategoryTotalCount, rightTbl, rightPred.where, rightPred.args,
	)
	if err != nil {
		res := e.baseResult(facts)
		captureDiagnostics(&res, opts, rightQuery, rightArgs)
		return res, categorizeExecutionError(ctx, err)
	}
	facts.ObservedRightCount = intFact(rightCount)

	equal := leftCount == rightCount
	failed := 0
	if !equal {
		failed = 1
	}
	res := Result{
		Kind:           KindReconcileCountsEqual,
		Name:           fmt.Sprintf("%s: left=%d right=%d", e.Name(), leftCount, rightCount),
		Success:        equal,
		RowDenominator: RowDenominatorUnavailable,
		FailedCount:    failed,
		Facts:          ResultFacts{Reconcile: facts},
		shape:          resultShapeReconcileCounts,
	}
	captureDiagnostics(&res, opts, leftQuery, leftArgs)
	return res, nil
}

// reconcileResultProfile reports whether res matches the ReconcileCounts result
// profile. Export uses this so successful reconciliations always publish
// counts.failed (including zero) without classifying arbitrary KindCustom
// results or generic KindReconcileCountsEqual shells. Display reuses the same
// profile via reconcileCountsDisplayFailed.
func reconcileResultProfile(res Result) bool {
	if res.shape != resultShapeReconcileCounts {
		return false
	}
	if res.Kind != KindReconcileCountsEqual || res.Err != nil {
		return false
	}
	if res.RowDenominator != RowDenominatorUnavailable || res.Column != "" {
		return false
	}
	if res.Facts.Reconcile == nil {
		return false
	}
	if len(res.SampleValues) > 0 || len(res.FailedKeys) > 0 {
		return false
	}
	return true
}

// reconcileCountsDisplayFailed returns the failed count when res matches the
// reconcile-count profile so rendering stays denominator-free.
func reconcileCountsDisplayFailed(res Result) (int, bool) {
	if !reconcileResultProfile(res) {
		return 0, false
	}
	return res.FailedCount, true
}

func (e reconcileCountsExpectation) baseResult(facts *ReconcileFacts) Result {
	return Result{
		Kind:           KindReconcileCountsEqual,
		Name:           e.Name(),
		RowDenominator: RowDenominatorUnavailable,
		Facts:          ResultFacts{Reconcile: facts},
	}
}

func tableRefDisplay(table TableRef) string {
	if table.Schema != "" {
		return table.Schema + "." + table.Name
	}
	return table.Name
}

var (
	_ Expectation     = reconcileCountsExpectation{}
	_ metaExpectation = reconcileCountsExpectation{}
)
