package mealai

import (
	"context"
	"strings"
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

func TestChatInitialFoodIdentitySpecificityIsAmountInvariant(t *testing.T) {
	const (
		foodID        = int64(42)
		displayName   = "Az yağlı krem peynir"
		canonicalName = "Low fat cream cheese"
		query         = "az yağlı krem peynir"
	)
	amountClarification := foodamount.Resolution{
		State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired,
		Clarification: &foodamount.Clarification{Portions: []food.Portion{}, AllowDirectGrams: true},
	}

	missingChat := &fakeChatInterpreter{initial: mealchat.InitialInterpretation{
		Purpose: mealchat.PurposeMealLogging,
		Items: []mealchat.InitialItem{{
			Evidence: "Az yağlı krem peynir", Intent: foodintent.FoodIntent{Query: query},
		}},
	}}
	missingResolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(foodID, displayName, canonicalName, nil)}}
	missingAmount := &fakeAmountResolver{results: []foodamount.Resolution{amountClarification}}
	missing, err := NewService(
		&fakeTextExtractor{}, nil, missingResolver, missingAmount, &fakeFoodDetailer{}, &fakeNutritionCalculator{}, missingChat,
	).Chat(context.Background(), ChatRequest{Message: "Az yağlı krem peynir yedim.", Locale: "tr-TR"})
	if err != nil {
		t.Fatal(err)
	}
	if missing.State != StateClarificationRequired || len(missing.Items) != 1 || missing.Items[0].Food == nil || missing.Items[0].Food.FoodID != foodID || missing.Items[0].Food.DisplayName != displayName {
		t.Fatalf("missing-amount result = %#v", missing)
	}
	if missing.Items[0].Clarification == nil || missing.Items[0].Clarification.Kind != ClarificationAmount || !strings.Contains(missing.Assistant.Text, displayName) {
		t.Fatalf("missing-amount clarification/assistant = %#v / %#v", missing.Items[0].Clarification, missing.Assistant)
	}
	if len(missingResolver.requests) != 1 || missingResolver.requests[0].Intent.Query != query || missingResolver.requests[0].Intent.Quantity != nil || missingResolver.requests[0].Intent.UnitHint != nil {
		t.Fatalf("missing-amount resolver request = %#v", missingResolver.requests)
	}

	quantity, unit := 150.0, "g"
	explicitIntent := foodintent.FoodIntent{Query: query, Quantity: &quantity, UnitHint: &unit}
	explicitChat := &fakeChatInterpreter{initial: mealchat.InitialInterpretation{
		Purpose: mealchat.PurposeMealLogging,
		Items: []mealchat.InitialItem{{
			Evidence: "150 g az yağlı krem peynir", Intent: explicitIntent,
		}},
	}}
	explicitResolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(foodID, displayName, canonicalName, nil)}}
	explicitAmount := &fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(foodID, quantity)}}
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: foodID, ResolvedGrams: quantity}}}
	explicit, err := NewService(
		&fakeTextExtractor{}, nil, explicitResolver, explicitAmount, &fakeFoodDetailer{}, calculator, explicitChat,
	).Chat(context.Background(), ChatRequest{Message: "150 g az yağlı krem peynir yedim.", Locale: "tr-TR"})
	if err != nil {
		t.Fatal(err)
	}
	if explicit.State != StateReady || len(explicit.Items) != 1 || explicit.Items[0].Food == nil || explicit.Items[0].Food.FoodID != foodID || explicit.Items[0].Food.DisplayName != displayName || explicit.Items[0].Preview == nil || explicit.Items[0].Preview.ResolvedGrams != quantity {
		t.Fatalf("explicit-amount result = %#v", explicit)
	}
	if len(explicitResolver.requests) != 1 || explicitResolver.requests[0].Intent.Query != query || explicitResolver.requests[0].Intent.Quantity == nil || *explicitResolver.requests[0].Intent.Quantity != quantity || explicitResolver.requests[0].Intent.UnitHint == nil || *explicitResolver.requests[0].Intent.UnitHint != unit {
		t.Fatalf("explicit-amount resolver request = %#v", explicitResolver.requests)
	}
	if missing.Items[0].Food.FoodID != explicit.Items[0].Food.FoodID || missingResolver.requests[0].Intent.Query != explicitResolver.requests[0].Intent.Query {
		t.Fatalf("identity changed with amount: missing=%#v explicit=%#v", missing.Items[0], explicit.Items[0])
	}
}

func TestChatAmountContinuationReplaysThenUsesResolveSelection(t *testing.T) {
	const (
		foodID        = int64(42)
		displayName   = "Az yağlı krem peynir"
		canonicalName = "Low fat cream cheese"
		query         = "az yağlı krem peynir"
	)
	intent := foodintent.FoodIntent{Query: query}
	grams := 150.0
	chat := &fakeChatInterpreter{
		initial:   mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: displayName, Intent: intent}}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationGrams, Grams: &grams}},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{
		resolvedIdentity(foodID, displayName, canonicalName, nil), resolvedIdentity(foodID, displayName, canonicalName, nil),
	}}
	clarification := foodamount.Resolution{State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired, Clarification: &foodamount.Clarification{Portions: []food.Portion{}, AllowDirectGrams: true}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{clarification, clarification, resolvedGrams(foodID, grams)}}
	detail := validDetail(foodID)
	detail.DisplayName, detail.Food.CanonicalName = displayName, canonicalName
	detailer := &fakeFoodDetailer{detail: detail}
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: foodID, ResolvedGrams: grams}}}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, detailer, calculator, chat)

	initial, err := service.Chat(context.Background(), ChatRequest{Message: "Az yağlı krem peynir yedim.", Locale: "tr-TR"})
	if err != nil || initial.State != StateClarificationRequired {
		t.Fatalf("initial/error = %#v / %v", initial, err)
	}
	if initial.Items[0].Food == nil || initial.Items[0].Food.FoodID != foodID || initial.Items[0].Food.DisplayName != displayName || !strings.Contains(initial.Assistant.Text, displayName) {
		t.Fatalf("initial identity/assistant = %#v / %#v", initial.Items[0], initial.Assistant)
	}
	continued, err := service.Chat(context.Background(), ChatRequest{Message: "150 gramdı", Locale: "tr-TR", State: &initial.NextState})
	if err != nil {
		t.Fatal(err)
	}
	if continued.State != StateReady || continued.Items[0].Food == nil || continued.Items[0].Food.FoodID != foodID || continued.Items[0].Food.DisplayName != displayName || continued.Items[0].Preview == nil || continued.Items[0].Preview.ResolvedGrams != grams || continued.NextState.Items[0].AmountChoice == nil || calculator.calls != 1 {
		t.Fatalf("continued/calls = %#v / %d", continued, calculator.calls)
	}
	if len(resolver.requests) != 2 || resolver.requests[0].Intent.Query != query || resolver.requests[1].Intent.Query != query {
		t.Fatalf("resolver requests = %#v", resolver.requests)
	}
	if len(amount.requests) != 3 || amount.requests[0].FoodID != foodID || amount.requests[1].FoodID != foodID || amount.requests[2].FoodID != foodID || amount.requests[2].Intent.Quantity == nil || *amount.requests[2].Intent.Quantity != grams {
		t.Fatalf("amount requests = %#v", amount.requests)
	}
	if len(calculator.requests) != 1 || calculator.requests[0].FoodID != foodID || calculator.requests[0].Grams == nil || *calculator.requests[0].Grams != grams {
		t.Fatalf("calculator requests = %#v", calculator.requests)
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

func TestChatFoodRephraseRerunsTrustedPipelineAndPreservesOnlyRawAmount(t *testing.T) {
	for _, test := range []struct {
		name                string
		message             string
		replacementEvidence string
		replacementQuantity *float64
		wantQuantity        float64
		wantAmountEvidence  bool
	}{
		{name: "independent prior amount", message: "ızgara tavuk göğsüydü", replacementEvidence: "ızgara tavuk göğsüydü", wantQuantity: 150, wantAmountEvidence: true},
		{name: "latest amount overrides prior", message: "aslında 200 g ızgara tavuk göğsüydü", replacementEvidence: "200 g ızgara tavuk göğsüydü", replacementQuantity: floatPointer(200), wantQuantity: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			originalQuantity, originalUnit := 150.0, "g"
			originalIntent := foodintent.FoodIntent{Query: "tavuk", Quantity: &originalQuantity, UnitHint: &originalUnit}
			replacementUnit := (*string)(nil)
			if test.replacementQuantity != nil {
				replacementUnit = stringPointer("g")
			}
			replacementIntent := foodintent.FoodIntent{Query: "ızgara tavuk göğsü", Quantity: test.replacementQuantity, UnitHint: replacementUnit}
			chat := &fakeChatInterpreter{
				initial: mealchat.InitialInterpretation{Purpose: mealchat.PurposeNutritionQuery, Items: []mealchat.InitialItem{{Evidence: "150 g tavuk gibi bir şey", Intent: originalIntent}}},
				decisions: []mealchat.ContinuationDecision{{
					Kind: mealchat.ContinuationFoodRephrase, ReplacementEvidence: &test.replacementEvidence, ReplacementIntent: &replacementIntent,
				}},
			}
			notFound := foodresolver.Resolution{State: foodresolver.StateNotFound, Reason: foodresolver.ReasonNoCandidates, Candidates: []foodsearch.FoodCandidate{}}
			resolver := &fakeFoodResolver{results: []foodresolver.Resolution{notFound, notFound, resolvedIdentity(7, "Izgara tavuk göğsü", "Grilled chicken breast", nil)}}
			amount := &fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(7, test.wantQuantity)}}
			calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: 7, ResolvedGrams: test.wantQuantity}}}
			service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, calculator, chat)

			initial, err := service.Chat(context.Background(), ChatRequest{Message: "150 g tavuk gibi bir şey", Locale: "tr"})
			if err != nil || initial.State != StateClarificationRequired || initial.Assistant.Kind != AssistantClarification || len(initial.Items[0].Clarification.Candidates) != 0 {
				t.Fatalf("initial/error = %#v / %v", initial, err)
			}
			continued, err := service.Chat(context.Background(), ChatRequest{Message: test.message, Locale: "tr", State: &initial.NextState})
			if err != nil || continued.State != StateReady || continued.Assistant.Kind != AssistantNutritionAnswer || amount.calls != 1 || calculator.calls != 1 {
				t.Fatalf("continued/error/calls = %#v / %v / %d/%d", continued, err, amount.calls, calculator.calls)
			}
			if amount.requests[0].Intent.Quantity == nil || *amount.requests[0].Intent.Quantity != test.wantQuantity || continued.NextState.Items[0].Evidence != test.replacementEvidence {
				t.Fatalf("amount/state = %#v / %#v", amount.requests, continued.NextState)
			}
			amountEvidence := continued.NextState.Items[0].AmountEvidence
			if test.wantAmountEvidence {
				if amountEvidence == nil || *amountEvidence != "150 g tavuk gibi bir şey" {
					t.Fatalf("amount evidence = %#v", amountEvidence)
				}
			} else if amountEvidence != nil {
				t.Fatalf("latest amount should use current evidence, got %#v", amountEvidence)
			}
			if strings.Contains(continued.NextState.Items[0].Evidence, "150 g +") {
				t.Fatalf("synthetic evidence = %q", continued.NextState.Items[0].Evidence)
			}
		})
	}
}

func TestFoodRephraseAlwaysClearsDerivedChoices(t *testing.T) {
	originalQuantity, unit := 150.0, "g"
	foodChoice, portionID, portionQuantity := int64(99), int64(21), 2.0
	replay := ConversationItemState{
		Position: 0, Evidence: "150 g tavuk", Intent: foodintent.FoodIntent{Query: "tavuk", Quantity: &originalQuantity, UnitHint: &unit},
		FoodChoiceID: &foodChoice, AmountChoice: &ExplicitChoice{Kind: ChoicePortion, PortionID: &portionID, Quantity: &portionQuantity},
	}
	replacementEvidence := "ızgara tavuk göğsüydü"
	replacementIntent := foodintent.FoodIntent{Query: "ızgara tavuk göğsü"}
	notFound := foodresolver.Resolution{State: foodresolver.StateNotFound, Reason: foodresolver.ReasonNoCandidates, Candidates: []foodsearch.FoodCandidate{}}
	service := NewService(&fakeTextExtractor{}, nil, &fakeFoodResolver{results: []foodresolver.Resolution{notFound}}, &fakeAmountResolver{}, &fakeFoodDetailer{}, &fakeNutritionCalculator{}, &fakeChatInterpreter{})
	_, err := service.applyFoodRephrase(context.Background(), "tr", mealchat.ContinuationDecision{
		Kind: mealchat.ContinuationFoodRephrase, ReplacementEvidence: &replacementEvidence, ReplacementIntent: &replacementIntent,
	}, &replay)
	if err != nil {
		t.Fatal(err)
	}
	if replay.FoodChoiceID != nil || replay.AmountChoice != nil || replay.AmountEvidence == nil || *replay.AmountEvidence != "150 g tavuk" {
		t.Fatalf("replay = %#v", replay)
	}
}

func TestConversationStateV2ValidatesAndClonesSeparateAmountEvidence(t *testing.T) {
	active := 0
	quantity, unit := 150.0, "g"
	amountEvidence := "150 g tavuk gibi bir şey"
	state := ConversationState{
		Version: ConversationVersion, Purpose: ChatPurposeMealLogging, ActiveItemIndex: &active,
		Items: []ConversationItemState{{
			Position: 0, Evidence: "ızgara tavuk göğsüydü", AmountEvidence: &amountEvidence,
			Intent: foodintent.FoodIntent{Query: "ızgara tavuk göğsü", Quantity: &quantity, UnitHint: &unit},
		}},
	}
	if err := validateConversationState(state); err != nil {
		t.Fatalf("validate v2: %v", err)
	}
	cloned := cloneConversationState(state)
	*cloned.Items[0].AmountEvidence = "değiştirildi"
	if *state.Items[0].AmountEvidence != amountEvidence {
		t.Fatalf("clone aliased amount evidence: %#v", state.Items[0].AmountEvidence)
	}

	v1 := state
	v1.Version = 1
	if err := validateConversationState(v1); err == nil {
		t.Fatal("version 1 continuation was accepted")
	}
	tampered := state
	tampered.Items = append([]ConversationItemState(nil), state.Items...)
	tamperedAmount := "200 g tavuk"
	tampered.Items[0].AmountEvidence = &tamperedAmount
	if err := validateConversationState(tampered); err == nil {
		t.Fatal("tampered amount evidence was accepted")
	}
}

func TestChatFoodRephraseDoesNotInheritIdentityDependentCount(t *testing.T) {
	originalQuantity, unit := 2.0, "adet"
	originalIntent := foodintent.FoodIntent{Query: "tavuk", Quantity: &originalQuantity, UnitHint: &unit}
	replacementEvidence := "ızgara tavuk göğsüydü"
	replacementIntent := foodintent.FoodIntent{Query: "ızgara tavuk göğsü"}
	chat := &fakeChatInterpreter{
		initial: mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{{Evidence: "2 tavuk gibi bir şey", Intent: originalIntent}}},
		decisions: []mealchat.ContinuationDecision{{
			Kind: mealchat.ContinuationFoodRephrase, ReplacementEvidence: &replacementEvidence, ReplacementIntent: &replacementIntent,
		}},
	}
	notFound := foodresolver.Resolution{State: foodresolver.StateNotFound, Reason: foodresolver.ReasonNoCandidates, Candidates: []foodsearch.FoodCandidate{}}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{notFound, notFound, resolvedIdentity(7, "Izgara tavuk göğsü", "Grilled chicken breast", nil)}}
	clarification := foodamount.Resolution{
		State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired,
		Clarification: &foodamount.Clarification{Portions: []food.Portion{}, AllowDirectGrams: true},
	}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{clarification}}
	calculator := &fakeNutritionCalculator{}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, calculator, chat)

	initial, err := service.Chat(context.Background(), ChatRequest{Message: "2 tavuk gibi bir şey", Locale: "tr"})
	if err != nil {
		t.Fatal(err)
	}
	continued, err := service.Chat(context.Background(), ChatRequest{Message: replacementEvidence, Locale: "tr", State: &initial.NextState})
	if err != nil || continued.State != StateClarificationRequired || continued.Items[0].Intent.Quantity != nil || continued.NextState.Items[0].AmountEvidence != nil || calculator.calls != 0 {
		t.Fatalf("continued/error/calculator = %#v / %v / %d", continued, err, calculator.calls)
	}
}

func TestChatMultiItemRephraseChangesOnlyFirstUnresolvedItem(t *testing.T) {
	grams, unit := 100.0, "g"
	appleIntent := foodintent.FoodIntent{Query: "elma", Quantity: &grams, UnitHint: &unit}
	chickenIntent := foodintent.FoodIntent{Query: "tavuk"}
	replacementEvidence := "ızgara tavuk göğsüydü"
	replacementIntent := foodintent.FoodIntent{Query: "ızgara tavuk göğsü"}
	chat := &fakeChatInterpreter{
		initial: mealchat.InitialInterpretation{Purpose: mealchat.PurposeMealLogging, Items: []mealchat.InitialItem{
			{Evidence: "100 g elma", Intent: appleIntent}, {Evidence: "tavuk gibi bir şey", Intent: chickenIntent},
		}},
		decisions: []mealchat.ContinuationDecision{{Kind: mealchat.ContinuationFoodRephrase, ReplacementEvidence: &replacementEvidence, ReplacementIntent: &replacementIntent}},
	}
	apple := resolvedIdentity(1, "Elma", "Apple", nil)
	notFound := foodresolver.Resolution{State: foodresolver.StateNotFound, Reason: foodresolver.ReasonNoCandidates, Candidates: []foodsearch.FoodCandidate{}}
	grilled := resolvedIdentity(2, "Izgara tavuk göğsü", "Grilled chicken breast", nil)
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{apple, notFound, apple, notFound, grilled}}
	clarification := foodamount.Resolution{
		State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired,
		Clarification: &foodamount.Clarification{Portions: []food.Portion{}, AllowDirectGrams: true},
	}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(1, 100), resolvedGrams(1, 100), clarification}}
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: 1, ResolvedGrams: 100}, {FoodID: 1, ResolvedGrams: 100}}}
	service := NewService(&fakeTextExtractor{}, nil, resolver, amount, &fakeFoodDetailer{}, calculator, chat)

	initial, err := service.Chat(context.Background(), ChatRequest{Message: "100 g elma ve tavuk gibi bir şey", Locale: "tr"})
	if err != nil || initial.ActiveItemIndex == nil || *initial.ActiveItemIndex != 1 {
		t.Fatalf("initial/error = %#v / %v", initial, err)
	}
	continued, err := service.Chat(context.Background(), ChatRequest{Message: replacementEvidence, Locale: "tr", State: &initial.NextState})
	if err != nil || continued.ActiveItemIndex == nil || *continued.ActiveItemIndex != 1 || continued.Items[0].State != ItemReady || continued.Items[1].Clarification == nil || continued.Items[1].Clarification.Kind != ClarificationAmount {
		t.Fatalf("continued/error = %#v / %v", continued, err)
	}
	if continued.NextState.Items[0].Evidence != "100 g elma" || continued.NextState.Items[1].Evidence != replacementEvidence || chat.initialCalls != 1 || chat.continuationCalls != 1 {
		t.Fatalf("state/calls = %#v / %d/%d", continued.NextState, chat.initialCalls, chat.continuationCalls)
	}
	if !strings.Contains(continued.Assistant.Text, "Izgara tavuk göğsü") || strings.Contains(continued.Assistant.Text, "Elma") {
		t.Fatalf("assistant = %#v", continued.Assistant)
	}
}

func validCandidates() []foodsearch.FoodCandidate {
	return []foodsearch.FoodCandidate{{FoodID: 1, DisplayName: "Çiğ tavuk", CanonicalName: "Raw chicken"}, {FoodID: 2, DisplayName: "Izgara tavuk", CanonicalName: "Grilled chicken"}}
}
