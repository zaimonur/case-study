package evaluation

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocale"
)

const datasetPath = "mealai-chat-v1.jsonl"

type datasetCase struct {
	ID       string        `json:"id"`
	Category string        `json:"category"`
	Tags     []string      `json:"tags"`
	Locale   string        `json:"locale"`
	Turns    []datasetTurn `json:"turns"`
	Notes    string        `json:"notes"`
}

type datasetTurn struct {
	Message string              `json:"message"`
	Expect  *datasetExpectation `json:"expect"`
}

type datasetExpectation struct {
	Purpose            string                `json:"purpose"`
	State              string                `json:"state"`
	ClarificationKind  string                `json:"clarification_kind"`
	ActiveItemIndex    *int                  `json:"active_item_index,omitempty"`
	MustNotAutoResolve *bool                 `json:"must_not_auto_resolve"`
	Items              []datasetExpectedItem `json:"items"`
}

type datasetExpectedItem struct {
	SourceOrder           *int     `json:"source_order"`
	ExpectedFoodID        *int64   `json:"expected_food_id,omitempty"`
	AllowedFoodIDs        []int64  `json:"allowed_food_ids,omitempty"`
	ExpectedCanonicalName string   `json:"expected_canonical_name,omitempty"`
	ExpectedSource        string   `json:"expected_source,omitempty"`
	ExpectedExternalID    string   `json:"expected_external_id,omitempty"`
	ExpectedResolvedGrams *float64 `json:"expected_resolved_grams,omitempty"`
}

func TestMealAIChatV1Dataset(t *testing.T) {
	cases := loadDataset(t)
	if len(cases) < 28 || len(cases) > 32 {
		t.Fatalf("case count = %d, want 28..32", len(cases))
	}

	validCategories := stringSet(
		"direct_auto_resolvable",
		"amount_clarification",
		"food_identity_ambiguity",
		"identity_specificity",
		"multi_food",
		"noise_typo_language",
		"unknown_non_food",
	)
	validPurposes := stringSet("meal_logging", "nutrition_query", "unknown")
	validStates := stringSet("ready", "clarification_required", "empty")
	validClarifications := stringSet("none", "amount", "food_identity")
	wantCategoryCounts := map[string]int{
		"direct_auto_resolvable":  7,
		"amount_clarification":    5,
		"food_identity_ambiguity": 5,
		"identity_specificity":    5,
		"multi_food":              3,
		"noise_typo_language":     3,
		"unknown_non_food":        2,
	}

	byID := make(map[string]datasetCase, len(cases))
	categoryCounts := make(map[string]int)
	for _, currentCase := range cases {
		if strings.TrimSpace(currentCase.ID) == "" {
			t.Fatal("case has an empty id")
		}
		if _, exists := byID[currentCase.ID]; exists {
			t.Fatalf("duplicate case id %q", currentCase.ID)
		}
		byID[currentCase.ID] = currentCase
		if !validCategories[currentCase.Category] {
			t.Fatalf("case %q has unsupported category %q", currentCase.ID, currentCase.Category)
		}
		categoryCounts[currentCase.Category]++
		if currentCase.Locale == "" {
			t.Fatalf("case %q has an empty locale", currentCase.ID)
		}
		parsedLocale, err := foodlocale.Parse(currentCase.Locale)
		if err != nil || parsedLocale.Exact != currentCase.Locale {
			t.Fatalf("case %q has invalid or non-normalized locale %q", currentCase.ID, currentCase.Locale)
		}
		if len(currentCase.Tags) == 0 {
			t.Fatalf("case %q has no tags", currentCase.ID)
		}
		assertUniqueNonEmptyStrings(t, currentCase.ID+" tags", currentCase.Tags)
		if len(currentCase.Turns) == 0 {
			t.Fatalf("case %q has no turns", currentCase.ID)
		}
		if strings.TrimSpace(currentCase.Notes) == "" {
			t.Fatalf("case %q has empty notes", currentCase.ID)
		}

		var casePurpose string
		for turnIndex, turn := range currentCase.Turns {
			location := fmt.Sprintf("case %q turn %d", currentCase.ID, turnIndex)
			if strings.TrimSpace(turn.Message) == "" {
				t.Fatalf("%s has an empty message", location)
			}
			if turn.Expect == nil {
				t.Fatalf("%s has no expectation", location)
			}
			expect := turn.Expect
			if !validPurposes[expect.Purpose] {
				t.Fatalf("%s has unsupported purpose %q", location, expect.Purpose)
			}
			if !validStates[expect.State] {
				t.Fatalf("%s has unsupported state %q", location, expect.State)
			}
			if !validClarifications[expect.ClarificationKind] {
				t.Fatalf("%s has unsupported clarification kind %q", location, expect.ClarificationKind)
			}
			if expect.MustNotAutoResolve == nil {
				t.Fatalf("%s omits must_not_auto_resolve", location)
			}
			if *expect.MustNotAutoResolve && expect.State == "ready" {
				t.Fatalf("%s contradicts must_not_auto_resolve with ready", location)
			}
			if expect.Items == nil {
				t.Fatalf("%s omits items", location)
			}

			if turnIndex == 0 {
				casePurpose = expect.Purpose
			} else {
				if currentCase.Turns[turnIndex-1].Expect.State != "clarification_required" {
					t.Fatalf("%s follows a turn that did not request clarification", location)
				}
				if expect.Purpose != casePurpose {
					t.Fatalf("%s changes purpose from %q to %q", location, casePurpose, expect.Purpose)
				}
			}

			switch expect.State {
			case "ready":
				if expect.ClarificationKind != "none" || expect.ActiveItemIndex != nil || len(expect.Items) == 0 {
					t.Fatalf("%s has incoherent ready outcome", location)
				}
			case "clarification_required":
				if expect.ClarificationKind == "none" || expect.ActiveItemIndex == nil || *expect.ActiveItemIndex < 0 || *expect.ActiveItemIndex >= len(expect.Items) {
					t.Fatalf("%s has incoherent clarification outcome", location)
				}
			case "empty":
				if expect.Purpose != "unknown" || expect.ClarificationKind != "none" || expect.ActiveItemIndex != nil || len(expect.Items) != 0 {
					t.Fatalf("%s has incoherent empty outcome", location)
				}
			}
			if expect.Purpose == "unknown" && expect.State != "empty" {
				t.Fatalf("%s labels unknown purpose without empty state", location)
			}

			for itemIndex, item := range expect.Items {
				itemLocation := fmt.Sprintf("%s item %d", location, itemIndex)
				if item.SourceOrder == nil || *item.SourceOrder != itemIndex {
					t.Fatalf("%s has source_order %v", itemLocation, item.SourceOrder)
				}
				if item.ExpectedFoodID != nil && *item.ExpectedFoodID <= 0 {
					t.Fatalf("%s has non-positive expected_food_id", itemLocation)
				}
				if item.ExpectedFoodID != nil && len(item.AllowedFoodIDs) != 0 {
					t.Fatalf("%s has both expected_food_id and allowed_food_ids", itemLocation)
				}
				assertAllowedFoodIDs(t, itemLocation, item.AllowedFoodIDs)
				hasIdentity := item.ExpectedFoodID != nil || len(item.AllowedFoodIDs) != 0
				if hasIdentity && strings.TrimSpace(item.ExpectedCanonicalName) == "" {
					t.Fatalf("%s has identity without expected_canonical_name", itemLocation)
				}
				if (item.ExpectedSource == "") != (item.ExpectedExternalID == "") {
					t.Fatalf("%s must pair expected_source and expected_external_id", itemLocation)
				}
				if hasIdentity && (item.ExpectedSource == "" || item.ExpectedExternalID == "") {
					t.Fatalf("%s has identity without a source anchor", itemLocation)
				}
				if item.ExpectedResolvedGrams != nil {
					grams := *item.ExpectedResolvedGrams
					if math.IsNaN(grams) || math.IsInf(grams, 0) || grams <= 0 {
						t.Fatalf("%s has invalid expected_resolved_grams %v", itemLocation, grams)
					}
					if !hasIdentity {
						t.Fatalf("%s has grams without canonical identity", itemLocation)
					}
				}
				if expect.State == "ready" && (!hasIdentity || item.ExpectedResolvedGrams == nil) {
					t.Fatalf("%s ready item lacks exact identity or grams", itemLocation)
				}
			}
			if expect.State == "clarification_required" && expect.ClarificationKind == "amount" {
				active := expect.Items[*expect.ActiveItemIndex]
				if active.ExpectedFoodID == nil && len(active.AllowedFoodIDs) == 0 {
					t.Fatalf("%s amount clarification lacks a resolved active identity", location)
				}
			}
		}
	}

	for category, want := range wantCategoryCounts {
		if got := categoryCounts[category]; got != want {
			t.Fatalf("category %q count = %d, want %d", category, got, want)
		}
	}
	assertCreamCheeseRegression(t, byID)
}

func loadDataset(t *testing.T) []datasetCase {
	t.Helper()
	file, err := os.Open(datasetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var cases []datasetCase
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			t.Fatalf("line %d is empty", lineNumber)
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var currentCase datasetCase
		if err := decoder.Decode(&currentCase); err != nil {
			t.Fatalf("decode line %d: %v", lineNumber, err)
		}
		if err := requireJSONEOF(decoder); err != nil {
			t.Fatalf("line %d: %v", lineNumber, err)
		}
		cases = append(cases, currentCase)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing content: %w", err)
	}
	return nil
}

func assertCreamCheeseRegression(t *testing.T, byID map[string]datasetCase) {
	t.Helper()
	direct := requiredCase(t, byID, "tr_low_fat_cream_cheese_direct_grams")
	continuation := requiredCase(t, byID, "tr_low_fat_cream_cheese_missing_amount")
	if len(direct.Turns) != 1 || len(continuation.Turns) != 2 {
		t.Fatal("low-fat cream-cheese regression cases have unexpected turn counts")
	}
	directID := requiredFoodID(t, direct.Turns[0], 0)
	firstID := requiredFoodID(t, continuation.Turns[0], 0)
	secondID := requiredFoodID(t, continuation.Turns[1], 0)
	if directID != firstID || firstID != secondID {
		t.Fatalf("low-fat cream-cheese FoodIDs differ: direct=%d first=%d second=%d", directID, firstID, secondID)
	}
	if directID != 461916 {
		t.Fatalf("low-fat cream-cheese FoodID = %d, want frozen catalog ID 461916", directID)
	}
	if continuation.Turns[0].Expect.State != "clarification_required" || continuation.Turns[0].Expect.ClarificationKind != "amount" {
		t.Fatal("low-fat cream-cheese first turn must request amount clarification")
	}
	for name, turn := range map[string]datasetTurn{"direct": direct.Turns[0], "continuation": continuation.Turns[1]} {
		grams := turn.Expect.Items[0].ExpectedResolvedGrams
		if turn.Expect.State != "ready" || grams == nil || *grams != 150 {
			t.Fatalf("low-fat cream-cheese %s ready grams are not 150", name)
		}
	}
}

func requiredCase(t *testing.T, cases map[string]datasetCase, id string) datasetCase {
	t.Helper()
	currentCase, exists := cases[id]
	if !exists {
		t.Fatalf("required case %q is missing", id)
	}
	return currentCase
}

func requiredFoodID(t *testing.T, turn datasetTurn, itemIndex int) int64 {
	t.Helper()
	if turn.Expect == nil || itemIndex >= len(turn.Expect.Items) || turn.Expect.Items[itemIndex].ExpectedFoodID == nil {
		t.Fatal("required expected_food_id is missing")
	}
	return *turn.Expect.Items[itemIndex].ExpectedFoodID
}

func assertAllowedFoodIDs(t *testing.T, location string, ids []int64) {
	t.Helper()
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			t.Fatalf("%s has non-positive allowed_food_id %d", location, id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("%s repeats allowed_food_id %d", location, id)
		}
		seen[id] = struct{}{}
	}
}

func assertUniqueNonEmptyStrings(t *testing.T, location string, values []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s contains an empty value", location)
		}
		if _, exists := seen[value]; exists {
			t.Fatalf("%s repeats %q", location, value)
		}
		seen[value] = struct{}{}
	}
}

func stringSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
