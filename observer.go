package gxsql

import (
	"errors"
	"time"
)

// QueryCategory identifies the purpose of an attempted SQL statement.
type QueryCategory string

const (
	QueryCategoryUnknown             QueryCategory = "unknown"
	QueryCategoryTotalCount          QueryCategory = "total_count"
	QueryCategoryFailureCount        QueryCategory = "failure_count"
	QueryCategorySample              QueryCategory = "sample"
	QueryCategoryFailedKeys          QueryCategory = "failed_keys"
	QueryCategoryAggregate           QueryCategory = "aggregate"
	QueryCategoryDistinctCount       QueryCategory = "distinct_count"
	QueryCategoryUniqueness          QueryCategory = "uniqueness"
	QueryCategoryStructuralDiscovery QueryCategory = "structural_discovery"
	QueryCategoryCustomCount         QueryCategory = "custom_count"
	QueryCategorySharedScalar        QueryCategory = "shared_scalar"
)

// QueryStatus describes the completed outcome of an attempted SQL statement.
type QueryStatus string

const (
	QueryStatusUnknown       QueryStatus = "unknown"
	QueryStatusSuccess       QueryStatus = "success"
	QueryStatusDatabaseError QueryStatus = "database_error"
	QueryStatusScanError     QueryStatus = "scan_error"
	QueryStatusContextError  QueryStatus = "context_error"
)

// QueryEvent contains privacy-safe metadata for one attempted SQL statement.
// SQL text, bound arguments, predicates, samples, and failed keys are never
// included.
type QueryEvent struct {
	ID       string
	Kind     ExpectationKind
	Category QueryCategory
	Duration time.Duration
	Status   QueryStatus
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
