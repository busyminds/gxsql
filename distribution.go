package gxsql

import (
	"context"
	"fmt"
	"math"
)

const populationStdDevCapabilityName = "aggregate.population_stddev"

// AggregateMetricsCapability describes optional exact aggregate algorithms
// advertised by a dialect. It does not widen [Dialect].
type AggregateMetricsCapability struct {
	// Name is the capability family label. Built-ins use "aggregate_metrics".
	Name string
	// PopulationStdDev reports whether the dialect advertises an exact
	// STDDEV_POP algorithm for [NumberColumn.StdDevBetween].
	PopulationStdDev bool
}

// AggregateMetricsDialect advertises exact aggregate algorithms supported by a
// dialect. Unsupported metric claims fail closed during suite preflight with
// [UnsupportedCapabilityError].
type AggregateMetricsDialect interface {
	// AggregateMetricsCapability returns the exact aggregate algorithms this
	// dialect advertises.
	AggregateMetricsCapability() AggregateMetricsCapability
}

func aggregateMetricsCapabilityFor(d Dialect) (AggregateMetricsCapability, bool) {
	ad, ok := d.(AggregateMetricsDialect)
	if !ok {
		return AggregateMetricsCapability{}, false
	}
	capability := ad.AggregateMetricsCapability()
	if capability.Name == "" {
		capability.Name = "aggregate_metrics"
	}
	return capability, true
}

func populationStdDevCapabilityError(kind ExpectationKind, d Dialect) error {
	capability, ok := aggregateMetricsCapabilityFor(d)
	if ok && capability.PopulationStdDev {
		return nil
	}
	return unsupportedCapabilityError(kind, d, populationStdDevCapabilityName)
}

func requiresPopulationStdDev(exp Expectation) bool {
	_, ok := unwrapExpectation(exp).(stdDevExpectation)
	return ok
}

// StdDevBetween returns a table-level expectation that the population standard
// deviation of non-NULL values lies in [lo, hi]. Dialects must advertise the
// exact STDDEV_POP algorithm before this expectation runs.
func (c NumberColumn) StdDevBetween(lo, hi float64) Expectation {
	return stdDevExpectation{column: c.column, lo: lo, hi: hi}
}

type stdDevExpectation struct {
	column string
	lo     float64
	hi     float64
}

func (e stdDevExpectation) Name() string {
	return fmt.Sprintf("%s population stddev in [%g,%g]", e.column, e.lo, e.hi)
}

func (e stdDevExpectation) expectationKind() ExpectationKind {
	return KindPopulationStdDevBetween
}

func (e stdDevExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	if math.IsNaN(e.lo) || math.IsInf(e.lo, 0) || math.IsNaN(e.hi) || math.IsInf(e.hi, 0) {
		return newConfigError(fmt.Errorf("standard-deviation bounds must be finite"))
	}
	if e.lo > e.hi {
		return newConfigError(fmt.Errorf("standard-deviation lower bound must not exceed upper bound"))
	}
	return nil
}

func (e stdDevExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	facts := ResultFacts{PopulationStdDev: &PopulationStdDevFacts{
		ConfiguredLower: floatFact(e.lo),
		ConfiguredUpper: floatFact(e.hi),
		Algorithm:       "STDDEV_POP",
		Exactness:       "exact_population",
	}}
	observed, ok, query, args, err := queryAggregateFloatWithArgs(ctx, db, table, opts, e.column, "STDDEV_POP")
	if err != nil {
		res := tableLevelResult(e.expectationKind(), e.column, e.Name(), false, facts)
		captureDiagnostics(&res, opts, query, args)
		return res, err
	}
	if !ok {
		res := tableLevelResult(e.expectationKind(), e.column, e.Name(), true, facts)
		captureDiagnostics(&res, opts, query, args)
		return res, nil
	}
	if math.IsNaN(observed) || math.IsInf(observed, 0) {
		err := &CategorizedError{Category: CategoryDatabase, Err: fmt.Errorf("population standard deviation is non-finite")}
		res := tableLevelResult(e.expectationKind(), e.column, e.Name(), false, facts)
		captureDiagnostics(&res, opts, query, args)
		return res, err
	}
	facts.PopulationStdDev.Observed = floatFact(observed)
	name := fmt.Sprintf("%s: got %g", e.Name(), observed)
	res := tableLevelResult(e.expectationKind(), e.column, name, observed >= e.lo && observed <= e.hi, facts)
	captureDiagnostics(&res, opts, query, args)
	return res, nil
}

func (postgresDialect) AggregateMetricsCapability() AggregateMetricsCapability {
	return AggregateMetricsCapability{Name: "aggregate_metrics", PopulationStdDev: true}
}

func (duckdbDialect) AggregateMetricsCapability() AggregateMetricsCapability {
	return AggregateMetricsCapability{Name: "aggregate_metrics", PopulationStdDev: true}
}

func (mysqlDialect) AggregateMetricsCapability() AggregateMetricsCapability {
	return AggregateMetricsCapability{Name: "aggregate_metrics", PopulationStdDev: true}
}

func (sqliteDialect) AggregateMetricsCapability() AggregateMetricsCapability {
	return AggregateMetricsCapability{Name: "aggregate_metrics", PopulationStdDev: false}
}
