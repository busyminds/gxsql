package gxsql

import (
	"errors"
	"time"
)

// QueryCategory identifies the purpose of an attempted SQL statement.
type QueryCategory string

const (
	// QueryCategoryUnknown marks an unclassified statement category.
	QueryCategoryUnknown QueryCategory = "unknown"
	// QueryCategoryTotalCount identifies COUNT(*) statements used for total or
	// numerator counts.
	QueryCategoryTotalCount QueryCategory = "total_count"
	// QueryCategoryFailureCount identifies failing-row COUNT(*) statements.
	QueryCategoryFailureCount QueryCategory = "failure_count"
	// QueryCategorySample identifies offending-value sample retrieval statements.
	QueryCategorySample QueryCategory = "sample"
	// QueryCategoryFailedKeys identifies capped in-report failed-key retrieval.
	QueryCategoryFailedKeys QueryCategory = "failed_keys"
	// QueryCategoryFailingKeys identifies complete failing-key retrieval
	// statements issued by [FailingKeys], distinct from capped in-report keys.
	QueryCategoryFailingKeys QueryCategory = "failing_keys"
	// QueryCategoryAggregate identifies table-level aggregate statements.
	QueryCategoryAggregate QueryCategory = "aggregate"
	// QueryCategoryDistinctCount identifies distinct-count aggregate statements.
	QueryCategoryDistinctCount QueryCategory = "distinct_count"
	// QueryCategoryUniqueness identifies uniqueness and duplicate-rate statements.
	QueryCategoryUniqueness QueryCategory = "uniqueness"
	// QueryCategoryStructuralDiscovery identifies zero-row schema probe statements.
	QueryCategoryStructuralDiscovery QueryCategory = "structural_discovery"
	// QueryCategoryCustomCount identifies trusted custom-count statements.
	QueryCategoryCustomCount QueryCategory = "custom_count"
	// QueryCategorySharedScalar identifies combined per-row failure-count statements.
	QueryCategorySharedScalar QueryCategory = "shared_scalar"
)

// QueryStatus describes the completed outcome of an attempted SQL statement.
type QueryStatus string

const (
	// QueryStatusUnknown marks an unclassified statement outcome.
	QueryStatusUnknown QueryStatus = "unknown"
	// QueryStatusSuccess marks a completed statement with no error.
	QueryStatusSuccess QueryStatus = "success"
	// QueryStatusDatabaseError marks a database/sql execution failure.
	QueryStatusDatabaseError QueryStatus = "database_error"
	// QueryStatusScanError marks a row iteration or column scan failure.
	QueryStatusScanError QueryStatus = "scan_error"
	// QueryStatusContextError marks context cancellation or deadline exceeded.
	QueryStatusContextError QueryStatus = "context_error"
)

// QueryEvent contains privacy-safe metadata for one attempted SQL statement.
// SQL text, bound arguments, predicates, samples, and failed keys are never
// included.
type QueryEvent struct {
	// ID is the caller-supplied expectation identifier from [WithID], when set.
	ID string
	// Kind is the library-defined expectation kind for the attempted statement.
	Kind ExpectationKind
	// Category identifies the purpose of the attempted SQL statement.
	Category QueryCategory
	// Duration is the monotonic elapsed time for the attempt.
	Duration time.Duration
	// Status is the completed outcome of the attempt.
	Status QueryStatus
}

// ObserverFunc receives completed SQL statement events synchronously.
type ObserverFunc func(QueryEvent)

type observerState struct {
	observer ObserverFunc
}

func (o *observerState) observe(
	start time.Time,
	id string,
	kind ExpectationKind,
	category QueryCategory,
	err error,
) (observerErr error) {
	if o == nil || o.observer == nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			observerErr = &CategorizedError{
				Category: CategoryObserver,
				Err:      errors.New("observer panicked"),
			}
		}
	}()
	o.observer(QueryEvent{
		ID:       id,
		Kind:     kind,
		Category: category,
		Duration: time.Since(start),
		Status:   queryStatus(err),
	})
	return nil
}

func queryStatus(err error) QueryStatus {
	if err == nil {
		return QueryStatusSuccess
	}
	if errors.Is(err, ErrCategoryContext) {
		return QueryStatusContextError
	}
	if errors.Is(err, ErrCategoryScan) {
		return QueryStatusScanError
	}
	return QueryStatusDatabaseError
}
