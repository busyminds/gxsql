package gxsql

import (
	"context"
	"fmt"
	"strings"
)

// WithID decorates an expectation with a caller-supplied stable [Result.ID].
// Blank or whitespace-only IDs are rejected during suite preflight. Duplicate
// IDs across one suite are also rejected before SQL runs. Wrapped behavior,
// [Result.Kind], and display [Result.Name] are preserved; IDs are never derived
// from Name.
func WithID(id string, exp Expectation) Expectation {
	return &idExpectation{id: id, inner: exp}
}

type idExpectation struct {
	id    string
	inner Expectation
}

func (e *idExpectation) Name() string {
	if e.inner == nil {
		return "<configuration error>"
	}
	return e.inner.Name()
}

func (e *idExpectation) expectationKind() ExpectationKind {
	return expectationKind(e.inner)
}

func (e *idExpectation) preflight() error {
	if strings.TrimSpace(e.id) == "" {
		return newConfigError(fmt.Errorf("expectation id is required"))
	}
	return preflightExpectation(e.inner)
}

func (e *idExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	res, err := e.inner.evaluateSQL(ctx, db, table, opts)
	res.ID = e.id
	if res.Kind == "" {
		res.Kind = expectationKind(e.inner)
	}
	return res, err
}
