package mealai

import (
	"context"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodresolver"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealchat"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type fakeChatInterpreter struct {
	initial           mealchat.InitialInterpretation
	decisions         []mealchat.ContinuationDecision
	initialCalls      int
	continuationCalls int
}

func (f *fakeChatInterpreter) InterpretInitial(context.Context, string) (mealchat.InitialInterpretation, error) {
	f.initialCalls++
	return f.initial, nil
}

func (f *fakeChatInterpreter) InterpretContinuation(context.Context, mealchat.ContinuationRequest) (mealchat.ContinuationDecision, error) {
	decision := f.decisions[f.continuationCalls]
	f.continuationCalls++
	return decision, nil
}

func TestChatInitialNutritionQueryUsesDeterministicPipeline(t *testing.T) {
	quantity, unit := 150.0, "g"
	intent := foodintent.FoodIntent{Query: "dana kıyma", Quantity: &quantity, UnitHint: &unit}
	chat := &fakeChatInterpreter{initial: mealchat.InitialInterpretation{
		Purpose: mealchat.PurposeNutritionQuery,
		Items:   []mealchat.InitialItem{{Evidence: "150 g dana kıyma", Intent: intent}},
	}}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(7, "Dana kıyma", "Ground beef", nil)}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(7, 150)}}
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: 7, ResolvedGrams: 150}}}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, calculator, chat)

	result, err := service.Chat(context.Background(), ChatRequest{Message: "150 g dana kıyma kaç kalori?", Locale: "tr-TR"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Purpose != ChatPurposeNutritionQuery || result.State != StateReady || len(result.Items) != 1 || result.Items[0].Preview == nil {
		t.Fatalf("result = %#v", result)
	}
	if calculator.calls != 1 || chat.continuationCalls != 0 || result.NextState.Items[0].AmountChoice != nil {
		t.Fatalf("calls/state = %d/%d/%#v", calculator.calls, chat.continuationCalls, result.NextState)
	}
}

func TestChatAmountContinuationReplaysThenUsesResolveSelection(t *testing.T) {
	intent := foodintent.FoodIntent{Query: "tavuk"}
	grams := 150.0
	chat := &fakeChatInterpreter{
		initial:   mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: "tavuk", Intent: intent}}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationGrams, Grams: &grams}},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{
		resolvedIdentity(7, "Tavuk", "Chicken", nil), resolvedIdentity(7, "Tavuk", "Chicken", nil),
	}}
	clarification := foodamount.Resolution{State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired, Clarification: &foodamount.Clarification{Portions: []food.Portion{}, AllowDirectGrams: true}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{clarification, clarification, resolvedGrams(7, 150)}}
	detailer := &fakeFoodDetailer{detail: validDetail(7)}
	calculator := &fakeNutritionCalculator{}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, detailer, calculator, chat)

	initial, err := service.Chat(context.Background(), ChatRequest{Message: "tavuk yedim", Locale: "tr-TR"})
	if err != nil || initial.State != StateClarificationRequired {
		t.Fatalf("initial/error = %#v / %v", initial, err)
	}
	continued, err := service.Chat(context.Background(), ChatRequest{Message: "150 gramdı", Locale: "tr-TR", State: &initial.NextState})
	if err != nil {
		t.Fatal(err)
	}
	if continued.State != StateReady || continued.Items[0].Preview == nil || continued.NextState.Items[0].AmountChoice == nil || calculator.calls != 1 {
		t.Fatalf("continued/calls = %#v / %d", continued, calculator.calls)
	}
}

func TestChatVagueAmountStaysUnresolvedWithoutNutrition(t *testing.T) {
	intent := foodintent.FoodIntent{Query: "tavuk"}
	chat := &fakeChatInterpreter{
		initial:   mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: "tavuk", Intent: intent}}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationUnresolved}},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(7, "Tavuk", "Chicken", nil), resolvedIdentity(7, "Tavuk", "Chicken", nil)}}
	clarification := foodamount.Resolution{State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired, Clarification: &foodamount.Clarification{Portions: []food.Portion{}, AllowDirectGrams: true}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{clarification, clarification}}
	calculator := &fakeNutritionCalculator{}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, calculator, chat)
	initial, _ := service.Chat(context.Background(), ChatRequest{Message: "tavuk yedim", Locale: "tr"})
	continued, err := service.Chat(context.Background(), ChatRequest{Message: "birazdı", Locale: "tr", State: &initial.NextState})
	if err != nil || continued.State != StateClarificationRequired || continued.ActiveItemIndex == nil || calculator.calls != 0 {
		t.Fatalf("continued/error/calls = %#v / %v / %d", continued, err, calculator.calls)
	}
}

func TestChatRejectsTamperedFoodChoiceAndStateMetadata(t *testing.T) {
	active := 0
	invalidFood := int64(999)
	state := ConversationState{
		Version: ConversationVersion, Purpose: ChatPurposeMealLogging, ActiveItemIndex: &active,
		Items: []ConversationItemState{{Position: 0, Evidence: "tavuk", Intent: foodintent.FoodIntent{Query: "tavuk"}, FoodChoiceID: &invalidFood}},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{{State: foodresolver.StateAmbiguous, Reason: foodresolver.ReasonMultipleExactIdentities, Candidates: validCandidates()}}}
	service := NewService(&fakeTextExtractor{}, nil, resolver, &fakeAmountResolver{}, &fakeFoodDetailer{}, &fakeNutritionCalculator{}, &fakeChatInterpreter{})
	_, err := service.Chat(context.Background(), ChatRequest{Message: "ızgara", Locale: "tr", State: &state})
	if !IsKind(err, ErrorInvalidInput) {
		t.Fatalf("tampered food error = %v", err)
	}
	state.Version++
	_, err = service.Chat(context.Background(), ChatRequest{Message: "ızgara", Locale: "tr", State: &state})
	if !IsKind(err, ErrorInvalidInput) {
		t.Fatalf("version error = %v", err)
	}
}

func TestChatFoodIdentityContinuationSelectsOnlyCurrentCandidate(t *testing.T) {
	intent := foodintent.FoodIntent{Query: "tavuk göğsü"}
	selectedID := int64(2)
	chat := &fakeChatInterpreter{
		initial:   mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: "tavuk göğsü", Intent: intent}}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationFoodIdentity, FoodID: &selectedID}},
	}
	ambiguous := foodresolver.Resolution{State: foodresolver.StateAmbiguous, Reason: foodresolver.ReasonMultipleExactIdentities, Candidates: validCandidates()}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{ambiguous, ambiguous}}
	amountClarification := foodamount.Resolution{State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired, Clarification: &foodamount.Clarification{Portions: []food.Portion{}, AllowDirectGrams: true}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{amountClarification}}
	detail := validDetail(selectedID)
	detail.DisplayName, detail.Food.CanonicalName = "Izgara tavuk", "Grilled chicken"
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{detail: detail}, &fakeNutritionCalculator{}, chat)
	initial, err := service.Chat(context.Background(), ChatRequest{Message: "tavuk göğsü yedim", Locale: "tr"})
	if err != nil {
		t.Fatal(err)
	}
	continued, err := service.Chat(context.Background(), ChatRequest{Message: "Izgara olan", Locale: "tr", State: &initial.NextState})
	if err != nil {
		t.Fatal(err)
	}
	if continued.State != StateClarificationRequired || continued.Items[0].Food == nil || continued.Items[0].Food.FoodID != selectedID || continued.Items[0].Clarification.Kind != ClarificationAmount {
		t.Fatalf("continued = %#v", continued)
	}
	if continued.NextState.Items[0].FoodChoiceID == nil || *continued.NextState.Items[0].FoodChoiceID != selectedID {
		t.Fatalf("next state = %#v", continued.NextState)
	}
}

func TestChatPortionContinuationUsesStoredPortionAndCalculator(t *testing.T) {
	intent := foodintent.FoodIntent{Query: "elma"}
	portionID, quantity := int64(21), 2.0
	portion := food.Portion{ID: portionID, FoodID: 7, Amount: 1, Measure: "adet", Grams: 120}
	chat := &fakeChatInterpreter{
		initial:   mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: "elma", Intent: intent}}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationPortion, PortionID: &portionID, Quantity: &quantity}},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(7, "Elma", "Apple", nil), resolvedIdentity(7, "Elma", "Apple", nil)}}
	clarification := foodamount.Resolution{State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired, Clarification: &foodamount.Clarification{Portions: []food.Portion{portion}, AllowDirectGrams: true}}
	selection := &foodamount.Selection{Kind: foodamount.SelectionPortion, FoodID: 7, Portion: &foodamount.PortionSelection{PortionID: portionID, Quantity: quantity, Amount: 1, Measure: "adet", PortionGrams: 120}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{clarification, clarification}, portionResults: []foodamount.Resolution{{State: foodamount.StateResolved, Reason: foodamount.ReasonExplicitPortionSelection, Selection: selection}}}
	detail := validDetail(7)
	detail.Portions = []food.Portion{portion}
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: 7, ResolvedGrams: 240}}}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{detail: detail}, calculator, chat)
	initial, _ := service.Chat(context.Background(), ChatRequest{Message: "elma yedim", Locale: "tr"})
	continued, err := service.Chat(context.Background(), ChatRequest{Message: "2 adet", Locale: "tr", State: &initial.NextState})
	if err != nil {
		t.Fatal(err)
	}
	if continued.State != StateReady || amount.portionCalls != 1 || calculator.calls != 1 || continued.Items[0].Preview.ResolvedGrams != 240 {
		t.Fatalf("continued/calls = %#v / %d / %d", continued, amount.portionCalls, calculator.calls)
	}
}

func TestChatPortionMeasureMismatchStaysUnresolvedWithoutCalculation(t *testing.T) {
	intent := foodintent.FoodIntent{Query: "ekmek"}
	wrongPortionID, quantity := int64(22), 2.0
	dilim := food.Portion{ID: 21, FoodID: 7, Amount: 1, Measure: "dilim", Grams: 30}
	bardak := food.Portion{ID: wrongPortionID, FoodID: 7, Amount: 1, Measure: "bardak", Grams: 100}
	chat := &fakeChatInterpreter{
		initial:   mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: "ekmek", Intent: intent}}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationPortion, PortionID: &wrongPortionID, Quantity: &quantity}},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(7, "Ekmek", "Bread", nil), resolvedIdentity(7, "Ekmek", "Bread", nil)}}
	clarification := foodamount.Resolution{State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired, Clarification: &foodamount.Clarification{Portions: []food.Portion{dilim, bardak}, AllowDirectGrams: true}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{clarification, clarification}}
	calculator := &fakeNutritionCalculator{}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, calculator, chat)
	initial, _ := service.Chat(context.Background(), ChatRequest{Message: "ekmek yedim", Locale: "tr"})
	continued, err := service.Chat(context.Background(), ChatRequest{Message: "2 dilim", Locale: "tr", State: &initial.NextState})
	if err != nil || continued.State != StateClarificationRequired || continued.ActiveItemIndex == nil || amount.portionCalls != 0 || calculator.calls != 0 {
		t.Fatalf("continued/error/portion/calculator = %#v / %v / %d / %d", continued, err, amount.portionCalls, calculator.calls)
	}
}

func TestChatMultiItemReplayKeepsFirstReadyAndSecondActive(t *testing.T) {
	grams, unit := 200.0, "g"
	apple := foodintent.FoodIntent{Query: "elma", Quantity: &grams, UnitHint: &unit}
	banana := foodintent.FoodIntent{Query: "muz"}
	chat := &fakeChatInterpreter{
		initial:   mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: "200 g elma", Intent: apple}, {Evidence: "muz", Intent: banana}}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationUnresolved}},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{
		resolvedIdentity(1, "Elma", "Apple", nil), resolvedIdentity(2, "Muz", "Banana", nil),
		resolvedIdentity(1, "Elma", "Apple", nil), resolvedIdentity(2, "Muz", "Banana", nil),
	}}
	clarification := foodamount.Resolution{State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired, Clarification: &foodamount.Clarification{Portions: []food.Portion{}, AllowDirectGrams: true}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(1, 200), clarification, resolvedGrams(1, 200), clarification}}
	calculator := &fakeNutritionCalculator{}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, calculator, chat)
	initial, _ := service.Chat(context.Background(), ChatRequest{Message: "200 g elma ve muz yedim", Locale: "tr"})
	if initial.ActiveItemIndex == nil || *initial.ActiveItemIndex != 1 {
		t.Fatalf("initial active = %#v", initial.ActiveItemIndex)
	}
	continued, err := service.Chat(context.Background(), ChatRequest{Message: "emin değilim", Locale: "tr", State: &initial.NextState})
	if err != nil || continued.ActiveItemIndex == nil || *continued.ActiveItemIndex != 1 || continued.Items[0].State != ItemReady || continued.Items[1].State != ItemClarificationRequired {
		t.Fatalf("continued/error = %#v / %v", continued, err)
	}
	if calculator.calls != 2 {
		t.Fatalf("calculator calls = %d, want trusted recalculation on both turns", calculator.calls)
	}
}

func TestChatUnknownInputIsEmptyWithoutDeterministicCalls(t *testing.T) {
	chat := &fakeChatInterpreter{initial: mealchat.InitialInterpretation{Purpose: mealchat.PurposeUnknown, Items: []mealchat.InitialItem{}}}
	resolver, amount, calculator := &fakeFoodResolver{}, &fakeAmountResolver{}, &fakeNutritionCalculator{}
	result, err := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, calculator, chat).
		Chat(context.Background(), ChatRequest{Message: "merhaba", Locale: "tr"})
	if err != nil || result.Purpose != ChatPurposeUnknown || result.State != StateEmpty || len(result.Items) != 0 || resolver.calls != 0 || amount.calls != 0 || calculator.calls != 0 {
		t.Fatalf("result/error/calls = %#v / %v / %d/%d/%d", result, err, resolver.calls, amount.calls, calculator.calls)
	}
}

func TestChatRejectsTamperedPortionQuantityOrderingAndActiveIndex(t *testing.T) {
	active := 0
	portionID, negative := int64(99), -2.0
	base := ConversationState{
		Version: ConversationVersion, Purpose: ChatPurposeMealLogging, ActiveItemIndex: &active,
		Items: []ConversationItemState{{Position: 0, Evidence: "elma", Intent: foodintent.FoodIntent{Query: "elma"}}},
	}
	for name, mutate := range map[string]func(*ConversationState){
		"negative quantity": func(state *ConversationState) {
			state.Items[0].AmountChoice = &ExplicitChoice{Kind: ChoicePortion, PortionID: &portionID, Quantity: &negative}
		},
		"altered ordering": func(state *ConversationState) { state.Items[0].Position = 1 },
		"malformed active": func(state *ConversationState) { invalid := 4; state.ActiveItemIndex = &invalid },
	} {
		t.Run(name, func(t *testing.T) {
			state := base
			state.Items = append([]ConversationItemState(nil), base.Items...)
			mutate(&state)
			service := NewService(&fakeTextExtractor{}, nil, &fakeFoodResolver{}, &fakeAmountResolver{}, &fakeFoodDetailer{}, &fakeNutritionCalculator{}, &fakeChatInterpreter{})
			_, err := service.Chat(context.Background(), ChatRequest{Message: "cevap", Locale: "tr", State: &state})
			if !IsKind(err, ErrorInvalidInput) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestChatRejectsReplayPortionOutsideCurrentStoredSet(t *testing.T) {
	active := 0
	portionID, quantity := int64(99), 1.0
	state := ConversationState{
		Version: ConversationVersion, Purpose: ChatPurposeMealLogging, ActiveItemIndex: &active,
		Items: []ConversationItemState{{
			Position: 0, Evidence: "elma", Intent: foodintent.FoodIntent{Query: "elma"},
			AmountChoice: &ExplicitChoice{Kind: ChoicePortion, PortionID: &portionID, Quantity: &quantity},
		}},
	}
	allowed := food.Portion{ID: 21, FoodID: 7, Amount: 1, Measure: "adet", Grams: 120}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(7, "Elma", "Apple", nil)}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{{
		State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired,
		Clarification: &foodamount.Clarification{Portions: []food.Portion{allowed}, AllowDirectGrams: true},
	}}}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, &fakeNutritionCalculator{}, &fakeChatInterpreter{})
	_, err := service.Chat(context.Background(), ChatRequest{Message: "bir adet", Locale: "tr", State: &state})
	if !IsKind(err, ErrorInvalidInput) || amount.portionCalls != 0 {
		t.Fatalf("error/portion calls = %v/%d", err, amount.portionCalls)
	}
}

func TestChatPortionContinuationReusesPriorExplicitQuantity(t *testing.T) {
	originalQuantity, unit := 2.0, "adet"
	intent := foodintent.FoodIntent{Query: "ekmek", Quantity: &originalQuantity, UnitHint: &unit}
	portionID := int64(21)
	portion := food.Portion{ID: portionID, FoodID: 7, Amount: 1, Measure: "dilim", Grams: 30}
	chat := &fakeChatInterpreter{
		initial:   mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: "2 ekmek", Intent: intent}}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationPortion, PortionID: &portionID}},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(7, "Ekmek", "Bread", nil), resolvedIdentity(7, "Ekmek", "Bread", nil)}}
	clarification := foodamount.Resolution{State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonUnsupportedUnitRequiresClarification, Clarification: &foodamount.Clarification{Portions: []food.Portion{portion}, AllowDirectGrams: true}}
	selection := &foodamount.Selection{Kind: foodamount.SelectionPortion, FoodID: 7, Portion: &foodamount.PortionSelection{PortionID: portionID, Quantity: 2, Amount: 1, Measure: "dilim", PortionGrams: 30}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{clarification, clarification}, portionResults: []foodamount.Resolution{{State: foodamount.StateResolved, Reason: foodamount.ReasonExplicitPortionSelection, Selection: selection}}}
	detail := validDetail(7)
	detail.Portions = []food.Portion{portion}
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: 7, ResolvedGrams: 60}}}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{detail: detail}, calculator, chat)

	initial, err := service.Chat(context.Background(), ChatRequest{Message: "2 ekmek yedim", Locale: "tr"})
	if err != nil {
		t.Fatal(err)
	}
	continued, err := service.Chat(context.Background(), ChatRequest{Message: "dilim olarak, saat 3'te yedim", Locale: "tr", State: &initial.NextState})
	if err != nil {
		t.Fatal(err)
	}
	if continued.State != StateReady || amount.portionCalls != 1 || amount.portionRequests[0].Quantity != 2 || calculator.calls != 1 || continued.Items[0].Preview.ResolvedGrams != 60 {
		t.Fatalf("continued/portion/calculator = %#v / %#v / %d", continued, amount.portionRequests, calculator.calls)
	}
}

func TestChatPortionContinuationDoesNotReuseMeasurementAsCount(t *testing.T) {
	originalQuantity := 150.0
	intent := foodintent.FoodIntent{Query: "ekmek", Quantity: &originalQuantity}
	portionID := int64(21)
	portion := food.Portion{ID: portionID, FoodID: 7, Amount: 1, Measure: "dilim", Grams: 30}
	chat := &fakeChatInterpreter{
		initial:   mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: "150 g ekmek", Intent: intent}}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationPortion, PortionID: &portionID}},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(7, "Ekmek", "Bread", nil), resolvedIdentity(7, "Ekmek", "Bread", nil)}}
	clarification := foodamount.Resolution{State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonUnsupportedUnitRequiresClarification, Clarification: &foodamount.Clarification{Portions: []food.Portion{portion}, AllowDirectGrams: true}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{clarification, clarification}}
	calculator := &fakeNutritionCalculator{}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, calculator, chat)

	initial, err := service.Chat(context.Background(), ChatRequest{Message: "150 g ekmek yedim", Locale: "tr"})
	if err != nil {
		t.Fatal(err)
	}
	continued, err := service.Chat(context.Background(), ChatRequest{Message: "dilim olarak", Locale: "tr", State: &initial.NextState})
	if err != nil || continued.State != StateClarificationRequired || continued.ActiveItemIndex == nil || amount.portionCalls != 0 || calculator.calls != 0 {
		t.Fatalf("continued/error/portion/calculator = %#v / %v / %d / %d", continued, err, amount.portionCalls, calculator.calls)
	}
}

func TestChatPortionContinuationLatestQuantityOverridesPriorQuantity(t *testing.T) {
	originalQuantity, unit := 2.0, "adet"
	override := 3.0
	intent := foodintent.FoodIntent{Query: "ekmek", Quantity: &originalQuantity, UnitHint: &unit}
	portionID := int64(21)
	portion := food.Portion{ID: portionID, FoodID: 7, Amount: 1, Measure: "dilim", Grams: 30}
	chat := &fakeChatInterpreter{
		initial:   mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: "2 ekmek", Intent: intent}}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationPortion, PortionID: &portionID, Quantity: &override}},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(7, "Ekmek", "Bread", nil), resolvedIdentity(7, "Ekmek", "Bread", nil)}}
	clarification := foodamount.Resolution{State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonUnsupportedUnitRequiresClarification, Clarification: &foodamount.Clarification{Portions: []food.Portion{portion}, AllowDirectGrams: true}}
	selection := &foodamount.Selection{Kind: foodamount.SelectionPortion, FoodID: 7, Portion: &foodamount.PortionSelection{PortionID: portionID, Quantity: 3, Amount: 1, Measure: "dilim", PortionGrams: 30}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{clarification, clarification}, portionResults: []foodamount.Resolution{{State: foodamount.StateResolved, Reason: foodamount.ReasonExplicitPortionSelection, Selection: selection}}}
	detail := validDetail(7)
	detail.Portions = []food.Portion{portion}
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: 7, ResolvedGrams: 90}}}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{detail: detail}, calculator, chat)
	initial, _ := service.Chat(context.Background(), ChatRequest{Message: "2 ekmek yedim", Locale: "tr"})
	continued, err := service.Chat(context.Background(), ChatRequest{Message: "3 dilim", Locale: "tr", State: &initial.NextState})
	if err != nil || continued.State != StateReady || amount.portionCalls != 1 || amount.portionRequests[0].Quantity != 3 || calculator.calls != 1 {
		t.Fatalf("continued/error/requests/calls = %#v / %v / %#v / %d", continued, err, amount.portionRequests, calculator.calls)
	}
}

func TestChatRejectsTamperedPriorQuantityBeforeNutrition(t *testing.T) {
	active := 0
	claimed, unit := 5.0, "adet"
	state := ConversationState{
		Version: ConversationVersion, Purpose: ChatPurposeMealLogging, ActiveItemIndex: &active,
		Items: []ConversationItemState{{Position: 0, Evidence: "2 ekmek", Intent: foodintent.FoodIntent{Query: "ekmek", Quantity: &claimed, UnitHint: &unit}}},
	}
	resolver, amount, calculator := &fakeFoodResolver{}, &fakeAmountResolver{}, &fakeNutritionCalculator{}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, calculator, &fakeChatInterpreter{})
	_, err := service.Chat(context.Background(), ChatRequest{Message: "dilim olarak", Locale: "tr", State: &state})
	if !IsKind(err, ErrorInvalidInput) || resolver.calls != 0 || amount.calls != 0 || calculator.calls != 0 {
		t.Fatalf("error/calls = %v / %d/%d/%d", err, resolver.calls, amount.calls, calculator.calls)
	}
}

func validCandidates() []foodsearch.FoodCandidate {
	return []foodsearch.FoodCandidate{{FoodID: 1, DisplayName: "Çiğ tavuk", CanonicalName: "Raw chicken"}, {FoodID: 2, DisplayName: "Izgara tavuk", CanonicalName: "Grilled chicken"}}
}
