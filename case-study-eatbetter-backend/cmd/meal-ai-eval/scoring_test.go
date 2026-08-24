package main

import "testing"

func TestDirectReadyScoringAndMismatches(t *testing.T) {
	expect := directExpectation(7, 150)
	tests := []struct {
		name             string
		actual           chatResponse
		canonicalCorrect int
		amountCorrect    int
		clarifyCorrect   int
		failure          string
	}{
		{name: "match", actual: readyChat("meal_logging", []int64{7}, []float64{150}), canonicalCorrect: 1, amountCorrect: 1, clarifyCorrect: 1},
		{name: "food id mismatch", actual: readyChat("meal_logging", []int64{8}, []float64{150}), amountCorrect: 1, clarifyCorrect: 1, failure: "food_id_mismatch"},
		{name: "amount mismatch", actual: readyChat("meal_logging", []int64{7}, []float64{149}), canonicalCorrect: 1, clarifyCorrect: 1, failure: "amount_mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostic, got := scoreResponse(0, expect, test.actual)
			if got.CanonicalCorrect != test.canonicalCorrect || got.CanonicalTotal != 1 ||
				got.AmountCorrect != test.amountCorrect || got.AmountTotal != 1 || got.ClarifyCorrect != test.clarifyCorrect || got.ClarifyTotal != 1 {
				t.Fatalf("counters = %#v", got)
			}
			if test.failure == "" && diagnostic.Outcome != "passed" {
				t.Fatalf("diagnostic = %#v", diagnostic)
			}
			if test.failure != "" && !containsFailure(diagnostic.AssertionFailures, test.failure) {
				t.Fatalf("failures = %#v, want %q", diagnostic.AssertionFailures, test.failure)
			}
		})
	}
}

func TestClarificationMatchAndMismatch(t *testing.T) {
	foodID := int64(7)
	expect := expectation{
		Purpose: "meal_logging", State: "clarification_required", ClarificationKind: "amount",
		ActiveItemIndex: intPointer(0), MustNotAutoResolve: boolPointer(false),
		Items: []expectedItem{{SourceOrder: intPointer(0), ExpectedFoodID: &foodID}},
	}
	diagnostic, got := scoreResponse(0, expect, clarificationChat("amount", &foodID))
	if diagnostic.Outcome != "passed" || got.CanonicalCorrect != 1 || got.ClarifyCorrect != 1 {
		t.Fatalf("match diagnostic/counters = %#v / %#v", diagnostic, got)
	}

	mismatch := clarificationChat("food_identity", nil)
	diagnostic, got = scoreResponse(0, expect, mismatch)
	if !containsFailure(diagnostic.AssertionFailures, "clarification_kind_mismatch") || got.ClarifyCorrect != 0 {
		t.Fatalf("mismatch diagnostic/counters = %#v / %#v", diagnostic, got)
	}
}

func TestUnsafeAutoResolutionAndUnknownEmpty(t *testing.T) {
	unsafeExpect := expectation{
		Purpose: "meal_logging", State: "clarification_required", ClarificationKind: "food_identity",
		ActiveItemIndex: intPointer(0), MustNotAutoResolve: boolPointer(true),
		Items: []expectedItem{{SourceOrder: intPointer(0)}},
	}
	diagnostic, got := scoreResponse(0, unsafeExpect, readyChat("meal_logging", []int64{7}, []float64{100}))
	if got.UnsafeTotal != 1 || got.UnsafeCount != 1 || !containsFailure(diagnostic.AssertionFailures, "unsafe_auto_resolution") {
		t.Fatalf("unsafe diagnostic/counters = %#v / %#v", diagnostic, got)
	}

	diagnostic, got = scoreResponse(0, unknownExpectation(), unknownChat())
	if diagnostic.Outcome != "passed" || got.UnsafeTotal != 1 || got.UnsafeCount != 0 || got.ClarifyCorrect != 1 {
		t.Fatalf("unknown diagnostic/counters = %#v / %#v", diagnostic, got)
	}
}

func TestMultiFoodArrayPositionMismatch(t *testing.T) {
	expect := expectation{
		Purpose: "meal_logging", State: "ready", ClarificationKind: "none", MustNotAutoResolve: boolPointer(false),
		Items: []expectedItem{
			{SourceOrder: intPointer(0), ExpectedFoodID: int64Pointer(1), ExpectedResolvedGrams: floatPointer(10)},
			{SourceOrder: intPointer(1), ExpectedFoodID: int64Pointer(2), ExpectedResolvedGrams: floatPointer(20)},
		},
	}
	diagnostic, got := scoreResponse(0, expect, readyChat("meal_logging", []int64{2, 1}, []float64{20, 10}))
	if got.CanonicalCorrect != 0 || got.CanonicalTotal != 2 || got.ClarifyCorrect != 0 ||
		!containsFailure(diagnostic.AssertionFailures, "source_order_mismatch") {
		t.Fatalf("diagnostic/counters = %#v / %#v", diagnostic, got)
	}
}

func TestProductUnavailableAssertionsRemainEligible(t *testing.T) {
	expect := directExpectation(7, 150)
	diagnostic, got := scoreUnavailableTurn(1, expect, "blocked_turn", "")
	if got.CanonicalTotal != 1 || got.CanonicalCorrect != 0 || got.AmountTotal != 1 || got.AmountCorrect != 0 ||
		got.ClarifyTotal != 1 || got.ClarifyCorrect != 0 || !containsFailure(diagnostic.AssertionFailures, "blocked_turn") {
		t.Fatalf("diagnostic/counters = %#v / %#v", diagnostic, got)
	}
}

func TestZeroDenominatorPercentageIsNull(t *testing.T) {
	metric := newMetric(0, 0)
	if metric.Percentage != nil {
		t.Fatalf("metric = %#v", metric)
	}
}

func containsFailure(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
