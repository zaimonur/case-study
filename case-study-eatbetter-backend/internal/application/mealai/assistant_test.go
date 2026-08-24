package mealai

import (
	"strings"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

func TestAssistantReadyResponsesComeOnlyFromMaterializedState(t *testing.T) {
	calories, _ := food.NewNutrientAmount(320)
	protein, _ := food.NewNutrientAmount(35.2)
	knownZero, _ := food.NewNutrientAmount(0)
	item := Item{
		State: ItemReady, Food: &ResolvedFood{FoodID: 7, DisplayName: "Dana kıyma", CanonicalName: "Ground beef"},
		Selection: &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 7, Grams: &foodamount.GramsSelection{Grams: 150}},
		Preview: &NutritionPreview{ResolvedGrams: 150, Nutrition: nutritioncalc.Nutrition{
			Calories: calories, Protein: protein, Fat: knownZero,
		}},
	}
	result := ChatResult{Purpose: ChatPurposeNutritionQuery, State: StateReady, Items: []Item{item}}
	assistant, err := buildAssistantResponse("tr", result)
	if err != nil {
		t.Fatal(err)
	}
	if assistant.Kind != AssistantNutritionAnswer || strings.Contains(assistant.Text, "?") || !strings.Contains(assistant.Text, "320 kcal") || !strings.Contains(assistant.Text, "35,2 g") || !strings.Contains(assistant.Text, "yağ: 0 g") {
		t.Fatalf("assistant = %#v", assistant)
	}
	if strings.Contains(assistant.Text, "karbonhidrat: 0") {
		t.Fatalf("unknown nutrient rendered as zero: %q", assistant.Text)
	}

	mealReady, err := buildAssistantResponse("tr", ChatResult{Purpose: ChatPurposeMealLogging, State: StateReady, Items: []Item{item}})
	if err != nil || mealReady.Kind != AssistantMealReady || strings.Contains(strings.ToLower(mealReady.Text), "kaydettim") {
		t.Fatalf("meal ready/error = %#v / %v", mealReady, err)
	}

	multi, err := buildAssistantResponse("tr", ChatResult{Purpose: ChatPurposeNutritionQuery, State: StateReady, Items: []Item{item, item}})
	if err != nil || multi.Text != "2 yiyecek için besin değerlerini hesapladım." || strings.Contains(multi.Text, "kcal") {
		t.Fatalf("multi/error = %#v / %v", multi, err)
	}
}

func TestAssistantUnknownCaloriesAndGuidance(t *testing.T) {
	protein, _ := food.NewNutrientAmount(4.5)
	item := Item{
		State: ItemReady, Food: &ResolvedFood{FoodID: 7, DisplayName: "Elma", CanonicalName: "Apple"},
		Selection: &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 7, Grams: &foodamount.GramsSelection{Grams: 100}},
		Preview:   &NutritionPreview{ResolvedGrams: 100, Nutrition: nutritioncalc.Nutrition{Protein: protein}},
	}
	answer, err := buildAssistantResponse("tr", ChatResult{Purpose: ChatPurposeNutritionQuery, State: StateReady, Items: []Item{item}})
	if err != nil || !strings.Contains(answer.Text, "güvenilir kalori değeri mevcut değil") || !strings.Contains(answer.Text, "Protein: 4,5 g") {
		t.Fatalf("answer/error = %#v / %v", answer, err)
	}
	guidance, err := buildAssistantResponse("en", ChatResult{Purpose: ChatPurposeUnknown, State: StateEmpty, Items: []Item{}})
	if err != nil || guidance.Kind != AssistantGuidance || guidance.Text != "You can tell me what you ate or ask about a food's nutrition." {
		t.Fatalf("guidance/error = %#v / %v", guidance, err)
	}
}

func TestAssistantIdentityClarificationHandlesCandidateCardinality(t *testing.T) {
	active := 0
	for _, test := range []struct {
		name       string
		candidates []FoodOption
		contains   []string
		excludes   string
	}{
		{name: "zero", candidates: []FoodOption{}, contains: []string{"güvenle eşleştiremedim", "açık tarif"}},
		{name: "single", candidates: []FoodOption{{FoodID: 9, DisplayName: "Izgara tavuk", CanonicalName: "Grilled chicken"}}, contains: []string{"Izgara tavuk", "Bunu mu kastettin"}, excludes: "Hangisini"},
		{name: "multiple bounded", candidates: []FoodOption{{FoodID: 1, DisplayName: "Bir"}, {FoodID: 2, DisplayName: "İki"}, {FoodID: 3, DisplayName: "Üç"}, {FoodID: 4, DisplayName: "Dört"}}, contains: []string{"Bir", "İki", "Üç"}, excludes: "Dört"},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := Item{State: ItemClarificationRequired, Clarification: &Clarification{Kind: ClarificationFoodIdentity, Candidates: test.candidates, Portions: []food.Portion{}}}
			assistant, err := buildAssistantResponse("tr", ChatResult{Purpose: ChatPurposeMealLogging, State: StateClarificationRequired, Items: []Item{item}, ActiveItemIndex: &active})
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range test.contains {
				if !strings.Contains(assistant.Text, expected) {
					t.Fatalf("assistant %q does not contain %q", assistant.Text, expected)
				}
			}
			if test.excludes != "" && strings.Contains(assistant.Text, test.excludes) {
				t.Fatalf("assistant %q contains %q", assistant.Text, test.excludes)
			}
		})
	}
}

func TestAssistantAmountPolicyUsesReasonAndTrustedPortions(t *testing.T) {
	active := 0
	quantity := 2.0
	for _, test := range []struct {
		name     string
		reason   foodamount.Reason
		intent   foodintent.FoodIntent
		portions []food.Portion
		contains []string
		excludes []string
	}{
		{name: "quantity without portions", reason: foodamount.ReasonQuantityRequired, contains: []string{"kaç gram"}, excludes: []string{"dilim", "bardak"}},
		{name: "quantity with trusted portion", reason: foodamount.ReasonQuantityRequired, portions: []food.Portion{{ID: 1, Measure: "dilim"}}, contains: []string{"Ne kadar", "dilim"}, excludes: []string{"bardak"}},
		{name: "unit keeps known quantity", reason: foodamount.ReasonUnitRequired, intent: foodintent.FoodIntent{Quantity: &quantity}, contains: []string{"2 Ekmek", "hangi ölçüyü"}, excludes: []string{"Ne kadar"}},
		{name: "volume", reason: foodamount.ReasonVolumeRequiresClarification, contains: []string{"doğrudan grama çeviremiyorum"}},
		{name: "unsupported", reason: foodamount.ReasonUnsupportedUnitRequiresClarification, contains: []string{"güvenle dönüştüremiyorum"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := Item{
				Intent: test.intent, State: ItemClarificationRequired,
				Food:          &ResolvedFood{FoodID: 7, DisplayName: "Ekmek", CanonicalName: "Bread"},
				Clarification: &Clarification{Kind: ClarificationAmount, Reason: string(test.reason), Candidates: []FoodOption{}, Portions: test.portions, AllowDirectGrams: true},
			}
			assistant, err := buildAssistantResponse("tr", ChatResult{Purpose: ChatPurposeMealLogging, State: StateClarificationRequired, Items: []Item{item}, ActiveItemIndex: &active})
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range test.contains {
				if !strings.Contains(assistant.Text, expected) {
					t.Fatalf("assistant %q does not contain %q", assistant.Text, expected)
				}
			}
			for _, excluded := range test.excludes {
				if strings.Contains(assistant.Text, excluded) {
					t.Fatalf("assistant %q contains invented/redundant %q", assistant.Text, excluded)
				}
			}
		})
	}
}

func TestAssistantAsksOnlyFirstUnresolvedItem(t *testing.T) {
	active := 1
	ready := Item{State: ItemReady, Food: &ResolvedFood{FoodID: 1, DisplayName: "Elma"}, Selection: &foodamount.Selection{}, Preview: &NutritionPreview{}}
	unresolved := Item{
		State: ItemClarificationRequired, Food: &ResolvedFood{FoodID: 2, DisplayName: "Muz"},
		Clarification: &Clarification{Kind: ClarificationAmount, Reason: string(foodamount.ReasonQuantityRequired), Candidates: []FoodOption{}, Portions: []food.Portion{}, AllowDirectGrams: true},
	}
	assistant, err := buildAssistantResponse("tr", ChatResult{Purpose: ChatPurposeMealLogging, State: StateClarificationRequired, Items: []Item{ready, unresolved}, ActiveItemIndex: &active})
	if err != nil || !strings.Contains(assistant.Text, "Muz") || strings.Contains(assistant.Text, "Elma") {
		t.Fatalf("assistant/error = %#v / %v", assistant, err)
	}
}
