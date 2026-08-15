package gxsql

import (
	"errors"
	"testing"
)

func TestTrustedParentFilterCopiesValuesOnConstruction(t *testing.T) {
	t.Parallel()

	caller := []any{"active", int64(1)}
	filter := TrustedParentFilter("customers-active", "status = ? AND version = ?", caller...)
	caller[0] = "mutated"
	caller[1] = int64(0)

	got, err := validateParentFilter(filter)
	if err != nil {
		t.Fatal(err)
	}
	if got.values[0] != "active" {
		t.Fatalf("value[0] = %v, want active", got.values[0])
	}
	if got.values[1] != int64(1) {
		t.Fatalf("value[1] = %v, want 1", got.values[1])
	}
}

func TestTrustedParentFilterRejectsBlankIdentity(t *testing.T) {
	t.Parallel()

	_, err := validateParentFilter(TrustedParentFilter("   ", "status = ?", "active"))
	if err == nil {
		t.Fatal("expected blank identity rejection")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("expected invalid_config, got %v", err)
	}
	if !errors.Is(err, errParentFilterIdentityRequired) {
		t.Fatalf("expected parent-filter identity error, got %v", err)
	}
}

func TestTrustedParentFilterArityMismatch(t *testing.T) {
	t.Parallel()

	_, err := validateParentFilter(TrustedParentFilter("customers-active", "status = ?", "a", "b"))
	if err == nil {
		t.Fatal("expected arity mismatch")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("expected invalid_config, got %v", err)
	}
}

func TestTrustedSecondaryFilterRejectsMissingPredicate(t *testing.T) {
	t.Parallel()

	_, err := validateSecondaryFilter(TrustedSecondaryFilter("served", "", nil...))
	if err == nil {
		t.Fatal("expected missing predicate rejection")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("expected invalid_config, got %v", err)
	}
	if !errors.Is(err, errSecondaryFilterPredicateRequired) {
		t.Fatalf("expected secondary-filter predicate error, got %v", err)
	}
}

func TestTrustedFiltersAreDistinctFromScope(t *testing.T) {
	t.Parallel()

	// Compile-time distinctness: these assignments must not type-check if
	// uncommented.
	// var _ ParentFilter = TrustedScope("id", "x = ?", 1)
	// var _ SecondaryFilter = TrustedScope("id", "x = ?", 1)
	// var _ Scope = TrustedParentFilter("id", "x = ?", 1)

	parent := TrustedParentFilter("customers-active", "status = ?", "active")
	secondary := TrustedSecondaryFilter("served-open", "open = ?", true)
	scope := TrustedScope("suite-scope", "tenant_id = ?", "t1")

	if parent.identity == "" || secondary.identity == "" || scope.identity == "" {
		t.Fatal("expected constructed identities")
	}
	if _, err := validateParentFilter(parent); err != nil {
		t.Fatal(err)
	}
	if _, err := validateSecondaryFilter(secondary); err != nil {
		t.Fatal(err)
	}
}
