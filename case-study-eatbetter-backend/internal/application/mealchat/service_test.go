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

func TestInitialInterpretationAcceptsCompactMeasurementEvidence(t *testing.T) {
	tests := []struct {
		evidence string
		quantity float64
		unit     string
		query    string
	}{
		{"150 g tavuk", 150, "g", "tavuk"},
		{"150g tavuk", 150, "g", "tavuk"},
		{"150 gr tavuk", 150, "gr", "tavuk"},
		{"150gr tavuk", 150, "gr", "tavuk"},
		{"150 gram tavuk", 150, "gram", "tavuk"},
		{"150gram tavuk", 150, "gram", "tavuk"},
		{"2 kg tavuk", 2, "kg", "tavuk"},
		{"2kg tavuk", 2, "kg", "tavuk"},
		{"2kilogram tavuk", 2, "kilogram", "tavuk"},
		{"1,5kg tavuk", 1.5, "kg", "tavuk"},
		{"200ml süt", 200, "ml", "süt"},
		{"200mililitre süt", 200, "mililitre", "süt"},
		{"0.5l süt", 0.5, "l", "süt"},
		{"0,5 l süt", 0.5, "litre", "süt"},
	}
	for _, test := range tests {
		t.Run(test.evidence, func(t *testing.T) {
			quantity, unit := test.quantity, test.unit
			provider := &stubInterpreter{initial: InitialInterpretation{
				Purpose: PurposeMealLogging,
				Items: []InitialItem{{Evidence: test.evidence, Intent: foodintent.FoodIntent{
					Query: test.query, Quantity: &quantity, UnitHint: &unit,
				}}},
			}}
			result, err := NewService(provider).InterpretInitial(context.Background(), test.evidence+" yedim")
			if err != nil {
				t.Fatalf("InterpretInitial: %v", err)
			}
			if len(result.Items) != 1 || result.Items[0].Intent.UnitHint == nil {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestInitialInterpretationRejectsInventedQuantityAndWrongCountUnit(t *testing.T) {
	for _, test := range []struct {
		name     string
		evidence string
		query    string
		quantity float64
		unit     string
		wantOK   bool
	}{
		{name: "invented vague quantity", evidence: "biraz tavuk", query: "tavuk", quantity: 150, unit: "g"},
		{name: "measurement overridden by count", evidence: "2 dilim ekmek", query: "ekmek", quantity: 2, unit: "adet"},
		{name: "measurement hidden in query overridden by count", evidence: "2 dilim ekmek", query: "dilim ekmek", quantity: 2, unit: "adet"},
		{name: "bare count", evidence: "2 yumurta", query: "yumurta", quantity: 2, unit: "adet", wantOK: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			quantity, unit := test.quantity, test.unit
			provider := &stubInterpreter{initial: InitialInterpretation{Purpose: PurposeMealLogging, Items: []InitialItem{{
				Evidence: test.evidence, Intent: foodintent.FoodIntent{Query: test.query, Quantity: &quantity, UnitHint: &unit},
			}}}}
			_, err := NewService(provider).InterpretInitial(context.Background(), test.evidence)
			if test.wantOK && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !test.wantOK && !IsKind(err, ErrorInvalidProviderOutput) {
				t.Fatalf("error = %v", err)
			}
		})
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

func TestContinuationGramsAcceptsCompactExplicitEvidence(t *testing.T) {
	for _, message := range []string{"150 g", "150g", "150 gr", "150gr", "150 gram", "150gram"} {
		t.Run(message, func(t *testing.T) {
			grams := 150.0
			request := ContinuationRequest{
				Message: message, Kind: ClarificationAmount, OriginalIntent: foodintent.FoodIntent{Query: "tavuk"},
				ResolvedFood: &ResolvedFood{FoodID: 7, DisplayName: "Tavuk", CanonicalName: "Chicken"},
				Candidates:   []FoodCandidate{}, Portions: []food.Portion{},
			}
			decision, err := NewService(&stubInterpreter{continuation: ContinuationDecision{Kind: ContinuationGrams, Grams: &grams}}).
				InterpretContinuation(context.Background(), request)
			if err != nil || decision.Grams == nil || *decision.Grams != 150 {
				t.Fatalf("decision/error = %#v / %v", decision, err)
			}
		})
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

func TestPortionDecisionRequiresSelectedMeasureEvidence(t *testing.T) {
	quantity := 2.0
	dilimID, bardakID := int64(21), int64(22)
	base := ContinuationRequest{
		Message: "2 dilim", Kind: ClarificationAmount, OriginalIntent: foodintent.FoodIntent{Query: "ekmek"},
		ResolvedFood: &ResolvedFood{FoodID: 7, DisplayName: "Ekmek", CanonicalName: "Bread"},
		Candidates:   []FoodCandidate{}, Portions: []food.Portion{
			{ID: dilimID, FoodID: 7, Amount: 1, Measure: "dilim", Grams: 30},
			{ID: bardakID, FoodID: 7, Amount: 1, Measure: "bardak", Grams: 100},
		},
	}
	for _, test := range []struct {
		name      string
		message   string
		portionID int64
		measure   string
		wantKind  ContinuationKind
	}{
		{name: "mismatched allowed portion", message: "2 dilim", portionID: bardakID, wantKind: ContinuationUnresolved},
		{name: "matching portion", message: "2 dilim", portionID: dilimID, wantKind: ContinuationPortion},
		{name: "adet alias", message: "2 tane", portionID: 31, measure: "adet", wantKind: ContinuationPortion},
		{name: "vague wording", message: "normal olan", portionID: dilimID, wantKind: ContinuationUnresolved},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := base
			request.Message = test.message
			if test.measure != "" {
				request.Portions = []food.Portion{{ID: test.portionID, FoodID: 7, Amount: 1, Measure: test.measure, Grams: 50}}
			}
			decision, err := NewService(&stubInterpreter{continuation: ContinuationDecision{
				Kind: ContinuationPortion, PortionID: &test.portionID, Quantity: &quantity,
			}}).InterpretContinuation(context.Background(), request)
			if err != nil {
				t.Fatalf("InterpretContinuation: %v", err)
			}
			if decision.Kind != test.wantKind {
				t.Fatalf("decision = %#v, want %s", decision, test.wantKind)
			}
		})
	}
}

func TestPortionDecisionReusesOrOverridesValidatedOriginalQuantity(t *testing.T) {
	originalQuantity, unit := 2.0, "adet"
	portionID := int64(21)
	base := ContinuationRequest{
		Message: "dilim olarak", Kind: ClarificationAmount,
		OriginalEvidence: "2 ekmek", OriginalIntent: foodintent.FoodIntent{Query: "ekmek", Quantity: &originalQuantity, UnitHint: &unit},
		ResolvedFood: &ResolvedFood{FoodID: 7, DisplayName: "Ekmek", CanonicalName: "Bread"},
		Candidates:   []FoodCandidate{}, Portions: []food.Portion{{ID: portionID, FoodID: 7, Amount: 1, Measure: "dilim", Grams: 30}},
	}
	decision, err := NewService(&stubInterpreter{continuation: ContinuationDecision{Kind: ContinuationPortion, PortionID: &portionID}}).
		InterpretContinuation(context.Background(), base)
	if err != nil || decision.Kind != ContinuationPortion || decision.Quantity == nil || *decision.Quantity != 2 {
		t.Fatalf("reuse decision/error = %#v / %v", decision, err)
	}

	override := 3.0
	base.Message = "3 dilim"
	decision, err = NewService(&stubInterpreter{continuation: ContinuationDecision{Kind: ContinuationPortion, PortionID: &portionID, Quantity: &override}}).
		InterpretContinuation(context.Background(), base)
	if err != nil || decision.Quantity == nil || *decision.Quantity != 3 {
		t.Fatalf("override decision/error = %#v / %v", decision, err)
	}

	adetID := int64(22)
	base.Message = "aslında 3 taneydi"
	base.Portions = []food.Portion{{ID: adetID, FoodID: 7, Amount: 1, Measure: "adet", Grams: 30}}
	decision, err = NewService(&stubInterpreter{continuation: ContinuationDecision{Kind: ContinuationPortion, PortionID: &adetID, Quantity: &override}}).
		InterpretContinuation(context.Background(), base)
	if err != nil || decision.Quantity == nil || *decision.Quantity != 3 {
		t.Fatalf("inflected override decision/error = %#v / %v", decision, err)
	}
}

func TestPortionDecisionRejectsTamperedOriginalQuantityEvidence(t *testing.T) {
	claimed, unit := 5.0, "adet"
	portionID := int64(21)
	request := ContinuationRequest{
		Message: "dilim olarak", Kind: ClarificationAmount,
		OriginalEvidence: "2 ekmek", OriginalIntent: foodintent.FoodIntent{Query: "ekmek", Quantity: &claimed, UnitHint: &unit},
		ResolvedFood: &ResolvedFood{FoodID: 7, DisplayName: "Ekmek", CanonicalName: "Bread"},
		Candidates:   []FoodCandidate{}, Portions: []food.Portion{{ID: portionID, FoodID: 7, Amount: 1, Measure: "dilim", Grams: 30}},
	}
	_, err := NewService(&stubInterpreter{continuation: ContinuationDecision{Kind: ContinuationPortion, PortionID: &portionID}}).
		InterpretContinuation(context.Background(), request)
	if !IsKind(err, ErrorInvalidInput) {
		t.Fatalf("error = %v", err)
	}
}
