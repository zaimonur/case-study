package mealchat

import (
	"context"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type stubInterpreter struct {
	initial      InitialInterpretation
	continuation ContinuationDecision
	err          error
}

func (s *stubInterpreter) InterpretInitial(context.Context, string) (InitialInterpretation, error) {
	return s.initial, s.err
}

func (s *stubInterpreter) InterpretContinuation(context.Context, ContinuationRequest) (ContinuationDecision, error) {
	return s.continuation, s.err
}

func TestInitialInterpretationSupportsNutritionQuestionWithExactEvidence(t *testing.T) {
	quantity, unit := 150.0, "gram"
	provider := &stubInterpreter{initial: InitialInterpretation{
		Purpose: PurposeNutritionQuery,
		Items: []InitialItem{{Evidence: "150 gram dana kıyma", Intent: foodintent.FoodIntent{
			Query: "dana kıyma", Quantity: &quantity, UnitHint: &unit,
		}}},
	}}
	result, err := NewService(provider).InterpretInitial(context.Background(), "150 gram dana kıyma kaç kalori?")
	if err != nil {
		t.Fatal(err)
	}
	if result.Purpose != PurposeNutritionQuery || len(result.Items) != 1 || result.Items[0].Intent.UnitHint == nil || *result.Items[0].Intent.UnitHint != "g" {
		t.Fatalf("result = %#v", result)
	}
}

func TestContinuationChoicesAreConstrainedAndRequireEvidence(t *testing.T) {
	intent := foodintent.FoodIntent{Query: "tavuk"}
	foodRequest := ContinuationRequest{
		Message: "Izgara olan", Kind: ClarificationFoodIdentity, OriginalIntent: intent,
		Candidates: []FoodCandidate{{FoodID: 1, DisplayName: "Çiğ", CanonicalName: "Raw"}, {FoodID: 2, DisplayName: "Izgara", CanonicalName: "Grilled"}},
		Portions:   []food.Portion{},
	}
	invalidID := int64(999)
	_, err := NewService(&stubInterpreter{continuation: ContinuationDecision{Kind: ContinuationFoodIdentity, FoodID: &invalidID}}).
		InterpretContinuation(context.Background(), foodRequest)
	if !IsKind(err, ErrorInvalidProviderOutput) {
		t.Fatalf("disallowed food error = %v", err)
	}

	grams := 150.0
	amountRequest := ContinuationRequest{
		Message: "birazdı", Kind: ClarificationAmount, OriginalIntent: intent,
		ResolvedFood: &ResolvedFood{FoodID: 7, DisplayName: "Tavuk", CanonicalName: "Chicken"},
		Candidates:   []FoodCandidate{}, Portions: []food.Portion{},
	}
	_, err = NewService(&stubInterpreter{continuation: ContinuationDecision{Kind: ContinuationGrams, Grams: &grams}}).
		InterpretContinuation(context.Background(), amountRequest)
	if !IsKind(err, ErrorInvalidProviderOutput) {
		t.Fatalf("invented grams error = %v", err)
	}

	amountRequest.Message = "yaklaşık 150 gramdı"
	decision, err := NewService(&stubInterpreter{continuation: ContinuationDecision{Kind: ContinuationGrams, Grams: &grams}}).
		InterpretContinuation(context.Background(), amountRequest)
	if err != nil || decision.Grams == nil || *decision.Grams != 150 {
		t.Fatalf("decision/error = %#v / %v", decision, err)
	}
}

func TestPortionDecisionRejectsUnownedOrDisallowedPortion(t *testing.T) {
	quantity, portionID := 2.0, int64(22)
	request := ContinuationRequest{
		Message: "2 porsiyon", Kind: ClarificationAmount, OriginalIntent: foodintent.FoodIntent{Query: "elma"},
		ResolvedFood: &ResolvedFood{FoodID: 7, DisplayName: "Elma", CanonicalName: "Apple"},
		Candidates:   []FoodCandidate{}, Portions: []food.Portion{{ID: 21, FoodID: 7, Amount: 1, Measure: "adet", Grams: 120}},
	}
	_, err := NewService(&stubInterpreter{continuation: ContinuationDecision{
		Kind: ContinuationPortion, PortionID: &portionID, Quantity: &quantity,
	}}).InterpretContinuation(context.Background(), request)
	if !IsKind(err, ErrorInvalidProviderOutput) {
		t.Fatalf("error = %v", err)
	}
}
