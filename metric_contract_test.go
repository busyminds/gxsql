package gxsql

import "testing"

func TestSpec07KindsAreDistinct(t *testing.T) {
	kinds := []ExpectationKind{
		KindSumBetween,
		KindPopulationStdDevBetween,
		KindCompletenessRate,
		KindDuplicateRate,
		KindValueFrequency,
		KindDominantShare,
	}
	seen := make(map[ExpectationKind]struct{}, len(kinds))
	for _, kind := range kinds {
		if _, ok := seen[kind]; ok {
			t.Fatalf("duplicate Spec 07 kind %q", kind)
		}
		seen[kind] = struct{}{}
	}
	if KindCompletenessRate == KindNotNull || KindDuplicateRate == KindUnique {
		t.Fatal("Spec 07 rate kinds must not reuse per-row kinds")
	}
}

func TestSpec07FactsRepresentAbsentAndZeroSeparately(t *testing.T) {
	zero := 0
	facts := ResultFacts{
		Sum:          &SumFacts{Observed: &zero},
		Completeness: &CompletenessFacts{TotalCount: &zero},
	}
	if facts.Sum.Observed == nil || *facts.Sum.Observed != 0 {
		t.Fatalf("zero sum fact = %#v", facts.Sum)
	}
	if facts.Completeness.Rate != nil {
		t.Fatalf("empty completeness rate should be absent: %#v", facts.Completeness)
	}
}
