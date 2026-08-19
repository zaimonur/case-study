package main

import (
	"strconv"
	"testing"

	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocalization"
)

func TestValidateApril2026Baseline(t *testing.T) {
	t.Parallel()
	candidates := make([]app.Candidate, 0, 13_652)
	nextID := 1
	for dataType, count := range map[string]int{
		"foundation_food": 428, "survey_fndds_food": 5431, "sr_legacy_food": 7793,
	} {
		for range count {
			candidates = append(candidates, app.Candidate{ExternalID: strconv.Itoa(nextID), DataType: dataType, CanonicalName: "Food"})
			nextID++
		}
	}
	if err := validateApril2026Baseline(candidates); err != nil {
		t.Fatalf("valid baseline rejected: %v", err)
	}
	candidates[0].DataType = "foundation_food"
	candidates[1].DataType = "foundation_food"
	// Force a deterministic type-count mismatch regardless of randomized map iteration above.
	candidates[0].DataType = "unexpected"
	if err := validateApril2026Baseline(candidates); err == nil {
		t.Fatal("changed baseline accepted")
	}
}
