package gxsql

import "testing"

func TestSpec07ExpectationKindsDistinct(t *testing.T) {
	kinds := []ExpectationKind{
		KindSumBetween,
		KindCompletenessRate,
		KindDuplicateRate,
		KindValueFrequency,
		KindDominantShare,
		KindPopulationStdDevBetween,
	}
	seen := make(map[ExpectationKind]struct{}, len(kinds))
	for _, kind := range kinds {
		if kind == "" {
			t.Fatalf("empty kind")
		}
		if _, ok := seen[kind]; ok {
			t.Fatalf("duplicate kind %q", kind)
		}
		seen[kind] = struct{}{}
	}
	if KindCompletenessRate == KindNotNull || KindDuplicateRate == KindUnique ||
		KindDuplicateRate == KindCompositeUnique || KindSumBetween == KindAverageBetween ||
		KindValueFrequency == KindIn || KindDominantShare == KindNotIn {
		t.Fatal("new metric kinds must remain distinct from existing related kinds")
	}
}

func TestSpec07MetricFactsAbsenceVsZero(t *testing.T) {
	var facts ResultFacts
	if facts.Sum != nil || facts.PopulationStdDev != nil || facts.Completeness != nil ||
		facts.DuplicateRate != nil || facts.Frequency != nil || facts.DominantShare != nil {
		t.Fatalf("zero ResultFacts nested metric pointers must be nil: %#v", facts)
	}

	zero := 0
	zeroRate := 0.0
	facts = ResultFacts{
		Sum:              &SumFacts{Observed: nil, ConfiguredLower: &zero, ConfiguredUpper: &zero},
		PopulationStdDev: &PopulationStdDevFacts{Observed: nil, ConfiguredLower: &zeroRate, ConfiguredUpper: &zeroRate},
		Completeness:     &CompletenessFacts{NonNullCount: &zero, TotalCount: &zero, Rate: nil},
		DuplicateRate:    &DuplicateRateFacts{DuplicateCount: &zero, TotalCount: &zero, Rate: &zeroRate},
		Frequency:        &FrequencyFacts{ValueCount: &zero, TotalCount: &zero, Share: nil},
		DominantShare:    &DominantShareFacts{DominantCount: &zero, TotalCount: &zero, Share: &zeroRate, TieCount: nil},
	}
	if facts.Sum.Observed != nil {
		t.Fatal("Sum.Observed nil must mean absence, not zero")
	}
	if facts.PopulationStdDev.Observed != nil {
		t.Fatal("PopulationStdDev.Observed nil must mean absence, not zero")
	}
	if facts.Completeness.Rate != nil {
		t.Fatal("Completeness.Rate nil must mean absence, not zero")
	}
	if facts.DuplicateRate.Rate == nil || *facts.DuplicateRate.Rate != 0 {
		t.Fatalf("DuplicateRate.Rate = %#v, want pointer to 0", facts.DuplicateRate.Rate)
	}
	if facts.Frequency.Share != nil {
		t.Fatal("Frequency.Share nil must mean absence, not zero")
	}
	if facts.DominantShare.TieCount != nil {
		t.Fatal("DominantShare.TieCount nil must mean unset, not zero")
	}
}
