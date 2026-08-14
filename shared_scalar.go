package gxsql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// sharedScalarMaxSelectTargets caps COUNT targets per shared statement so
// combined queries stay under PostgreSQL's MaxTupleAttributeNumber (1664).
// Tests in this package may lower it temporarily.
var sharedScalarMaxSelectTargets = 1024

// sharedScalarPlan is one compatible per-row slot included in a combined
// failure-count statement.
type sharedScalarPlan struct {
	id        string
	kind      ExpectationKind
	name      string
	column    string
	facts     ResultFacts
	maxFailed *int
	policy    *Policy
	inner     perRowExpectation
}

// sharedScalarBatch is one contiguous run of compatible per-row slots that may
// share a conditional-aggregate statement.
type sharedScalarBatch struct {
	indices []int
	plans   []sharedScalarPlan
}

func sharedScalarPlanFor(exp Expectation, opts evalOptions) (sharedScalarPlan, bool, error) {
	var maxFailed *int
	var policy *Policy
	cur := exp
	for {
		switch w := cur.(type) {
		case *idExpectation:
			if w == nil || w.inner == nil {
				return sharedScalarPlan{}, false, nil
			}
			cur = w.inner
		case *maxFailedCountExpectation:
			if w == nil || w.inner == nil {
				return sharedScalarPlan{}, false, nil
			}
			m := w.max
			maxFailed = &m
			cur = w.inner
		case *policyExpectation:
			if w == nil || w.inner == nil {
				return sharedScalarPlan{}, false, nil
			}
			p := w.policy
			normalized, err := normalizePolicy(p)
			if err != nil {
				return sharedScalarPlan{}, false, err
			}
			policy = &normalized
			cur = w.inner
		case *eligibilityExpectation:
			// Eligible rules use a per-rule effective population and total;
			// keep them on the sequential evaluation path.
			return sharedScalarPlan{}, false, nil
		default:
			goto built
		}
	}
built:
	inner, ok := cur.(perRowExpectation)
	if !ok {
		return sharedScalarPlan{}, false, nil
	}
	if inner.build == nil && inner.buildColumns == nil {
		return sharedScalarPlan{}, false, nil
	}
	// Prove the predicate can be rendered for this dialect/scope before
	// admitting the slot into the shared statement.
	if _, err := buildPerRowFailPredicate(inner, opts.dialect, opts.scope); err != nil {
		return sharedScalarPlan{}, false, err
	}
	return sharedScalarPlan{
		id:        expectationID(exp),
		kind:      inner.kind,
		name:      inner.name,
		column:    inner.column,
		facts:     inner.facts,
		maxFailed: maxFailed,
		policy:    policy,
		inner:     inner,
	}, true, nil
}

func buildPerRowFailPredicate(e perRowExpectation, d Dialect, scope *trustedScope) (rowPredicate, error) {
	if e.buildColumns != nil {
		return e.buildColumns(d, e.column, e.rightColumn, scope)
	}
	if e.build == nil {
		return rowPredicate{}, fmt.Errorf("gxsql: missing per-row predicate builder")
	}
	return e.build(d, e.column, scope)
}

// groupContiguousSharedBatches keeps only contiguous compatible runs of length
// >= 2 so intervening incompatible expectations preserve declaration order.
func groupContiguousSharedBatches(order []int, plans map[int]sharedScalarPlan) []sharedScalarBatch {
	var batches []sharedScalarBatch
	var cur sharedScalarBatch
	flush := func() {
		if len(cur.indices) >= 2 {
			batches = append(batches, cur)
		}
		cur = sharedScalarBatch{}
	}
	for _, idx := range order {
		if len(cur.indices) > 0 && idx != cur.indices[len(cur.indices)-1]+1 {
			flush()
		}
		cur.indices = append(cur.indices, idx)
		cur.plans = append(cur.plans, plans[idx])
	}
	flush()
	return batches
}

// evalSharedScalarCounts executes conditional-aggregate failure-count
// statement(s) for plans. Shared statement/total errors attribute to every
// slot in the failing chunk. Per-expectation diagnostic failures attribute
// only to the affected Result.Err. Without continueOnError, the first
// execution or diagnostic error returns immediately.
func evalSharedScalarCounts(
	ctx context.Context,
	db DB,
	table TableRef,
	opts evalOptions,
	plans []sharedScalarPlan,
) ([]Result, error) {
	results := make([]Result, len(plans))
	for i, plan := range plans {
		results[i] = applySharedPlanPolicy(Result{
			ID:             plan.id,
			Kind:           plan.kind,
			Name:           plan.name,
			Column:         plan.column,
			RowDenominator: RowDenominatorUnavailable,
			Facts:          plan.facts,
		}, plan)
	}
	if len(plans) == 0 {
		return results, nil
	}

	tbl, err := renderTable(opts.dialect, table)
	if err != nil {
		err = categorizeRenderError(err)
		for i := range results {
			results[i].Err = err
		}
		return results, err
	}

	scopePred, err := composeRowPredicateWithScope(opts.scope, rowPredicate{}, opts.dialect)
	if err != nil {
		err = categorizeRenderError(err)
		for i := range results {
			results[i].Err = err
		}
		return results, err
	}

	maxTargets := sharedScalarMaxSelectTargets
	if maxTargets < 1 {
		maxTargets = 1
	}

	for start := 0; start < len(plans); start += maxTargets {
		end := start + maxTargets
		if end > len(plans) {
			end = len(plans)
		}
		if err := evalSharedScalarCountChunk(ctx, db, table, tbl, opts, scopePred, plans[start:end], results[start:end]); err != nil {
			return results, err
		}
	}
	return results, nil
}

func evalSharedScalarCountChunk(
	ctx context.Context,
	db DB,
	table TableRef,
	tbl string,
	opts evalOptions,
	scopePred rowPredicate,
	plans []sharedScalarPlan,
	results []Result,
) error {
	scopeArgs := append([]any(nil), scopePred.args...)
	selectParts := make([]string, 0, len(plans))
	predArgs := make([]any, 0, len(plans)*2)
	scopedFailPreds := make([]rowPredicate, len(plans))
	questionMark := dialectUsesQuestionMark(opts.dialect)

	for i, plan := range plans {
		rawPred, err := buildPerRowFailPredicate(plan.inner, opts.dialect, nil)
		if err != nil {
			err = categorizeRenderError(err)
			for j := range results {
				results[j].Err = err
			}
			return err
		}
		shiftedWhere := rawPred.where
		if !questionMark {
			// Numbered placeholders: scope occupies $1..$n in WHERE; SELECT
			// predicates continue after scope + prior predicate args.
			shiftedWhere = shiftPositionalPlaceholders(opts.dialect, rawPred.where, len(scopeArgs)+len(predArgs))
		}
		selectParts = append(selectParts, "COUNT(CASE WHEN ("+shiftedWhere+") THEN 1 END)")
		predArgs = append(predArgs, rawPred.args...)

		scopedPred, err := buildPerRowFailPredicate(plan.inner, opts.dialect, opts.scope)
		if err != nil {
			err = categorizeRenderError(err)
			for j := range results {
				results[j].Err = err
			}
			return err
		}
		failPred, err := composeRowPredicateWithScope(opts.scope, scopedPred, opts.dialect)
		if err != nil {
			err = categorizeRenderError(err)
			for j := range results {
				results[j].Err = err
			}
			return err
		}
		scopedFailPreds[i] = failPred
	}

	query := "SELECT " + strings.Join(selectParts, ", ") + " FROM " + tbl
	if scopePred.where != "" {
		query += " WHERE " + scopePred.where
	}

	// Question-mark dialects bind left-to-right in SQL appearance order:
	// SELECT predicate placeholders precede the trailing WHERE scope.
	// Numbered dialects keep scope args first to match $1..$n in WHERE.
	var args []any
	if questionMark {
		args = append(append([]any(nil), predArgs...), scopeArgs...)
	} else {
		args = append(append([]any(nil), scopeArgs...), predArgs...)
	}

	for i := range results {
		captureDiagnostics(&results[i], opts, query, args)
	}

	total, err := resolveScopedTotal(ctx, db, table, opts)
	if err != nil {
		for i := range results {
			results[i].Err = err
		}
		return err
	}

	counts, err := queryScalarInts(ctx, db, query, len(plans), args...)
	if err != nil {
		for i := range results {
			results[i].Err = err
		}
		return err
	}

	for i, plan := range plans {
		res := perRowResult(plan.kind, plan.column, plan.name, total, counts[i], plan.facts)
		res.ID = plan.id
		captureDiagnostics(&res, opts, query, args)
		if counts[i] > 0 {
			if opts.sampleCap > 0 {
				samples, sampleErr := queryColumnSamples(ctx, db, tbl, plan.column, scopedFailPreds[i], opts, opts.sampleCap)
				if sampleErr != nil {
					res.Err = sampleErr
					res.Success = false
					results[i] = applySharedPlanPolicy(res, plan)
					if !opts.continueOnError {
						return sampleErr
					}
					continue
				}
				res.SampleValues = samples
			}
			if !opts.summaryOnly && len(opts.keyColumns) > 0 {
				keys, keyErr := queryFailedKeys(ctx, db, tbl, opts, scopedFailPreds[i])
				if keyErr != nil {
					res.Err = keyErr
					res.Success = false
					results[i] = applySharedPlanPolicy(res, plan)
					if !opts.continueOnError {
						return keyErr
					}
					continue
				}
				res.FailedKeys = keys
			}
		}
		// Apply policy after diagnostics so error results stay untolerated.
		results[i] = applySharedPlanPolicy(res, plan)
	}
	return nil
}

func applySharedPlanPolicy(res Result, plan sharedScalarPlan) Result {
	if plan.policy != nil {
		res = applyPolicy(res, *plan.policy)
	}
	if plan.maxFailed != nil {
		res = applyMaxFailedCount(res, *plan.maxFailed)
	}
	return res
}

func dialectUsesQuestionMark(d Dialect) bool {
	return d == nil || d.Placeholder(1) == "?"
}

func queryScalarInts(ctx context.Context, db DB, query string, n int, args ...any) ([]int, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, categorizeExecutionError(ctx, err)
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, categorizeScanError(ctx, err)
		}
		if err := rows.Close(); err != nil {
			return nil, categorizeScanError(ctx, err)
		}
		return nil, categorizeScanError(ctx, sql.ErrNoRows)
	}
	nulls := make([]sql.NullInt64, n)
	ptrs := make([]any, n)
	for i := range nulls {
		ptrs[i] = &nulls[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		_ = rows.Close()
		return nil, categorizeScanError(ctx, err)
	}
	if err := finishRowsRead(ctx, rows); err != nil {
		return nil, err
	}
	out := make([]int, n)
	for i, v := range nulls {
		if v.Valid {
			out[i] = int(v.Int64)
		}
	}
	return out, nil
}

// shiftPositionalPlaceholders rewrites $n placeholders by +delta for dialects
// that use numbered placeholders. Question-mark dialects are unchanged.
func shiftPositionalPlaceholders(d Dialect, where string, delta int) string {
	if delta == 0 || where == "" || dialectUsesQuestionMark(d) {
		return where
	}
	var b strings.Builder
	b.Grow(len(where) + 8)
	for i := 0; i < len(where); {
		if where[i] != '$' {
			b.WriteByte(where[i])
			i++
			continue
		}
		j := i + 1
		if j >= len(where) || where[j] < '1' || where[j] > '9' {
			b.WriteByte(where[i])
			i++
			continue
		}
		for j < len(where) && where[j] >= '0' && where[j] <= '9' {
			j++
		}
		n, convErr := strconv.Atoi(where[i+1 : j])
		if convErr != nil {
			b.WriteString(where[i:j])
			i = j
			continue
		}
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(n + delta))
		i = j
	}
	return b.String()
}
