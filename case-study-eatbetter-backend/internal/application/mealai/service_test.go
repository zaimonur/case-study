package mealai

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodresolver"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type fakeTextExtractor struct {
	result   foodextraction.TextFoodExtraction
	err      error
	calls    int
	texts    []string
	contexts []context.Context
}

func (fake *fakeTextExtractor) Extract(ctx context.Context, text string) (foodextraction.TextFoodExtraction, error) {
	fake.calls++
	fake.texts = append(fake.texts, text)
	fake.contexts = append(fake.contexts, ctx)
	return fake.result, fake.err
}

type fakeFoodResolver struct {
	results  []foodresolver.Resolution
	errs     []error
	calls    int
	requests []foodresolver.Request
	contexts []context.Context
}

func (fake *fakeFoodResolver) Resolve(ctx context.Context, request foodresolver.Request) (foodresolver.Resolution, error) {
	index := fake.calls
	fake.calls++
	fake.requests = append(fake.requests, request)
	fake.contexts = append(fake.contexts, ctx)
	if index < len(fake.errs) && fake.errs[index] != nil {
		return foodresolver.Resolution{}, fake.errs[index]
	}
	return fake.results[index], nil
}

type fakeAmountResolver struct {
	results         []foodamount.Resolution
	errs            []error
	calls           int
	requests        []foodamount.Request
	contexts        []context.Context
	portionResults  []foodamount.Resolution
	portionErrs     []error
	portionCalls    int
	portionRequests []foodamount.PortionSelectionRequest
	portionContexts []context.Context
}

func (fake *fakeAmountResolver) ResolvePortionSelection(ctx context.Context, request foodamount.PortionSelectionRequest) (foodamount.Resolution, error) {
	index := fake.portionCalls
	fake.portionCalls++
	fake.portionRequests = append(fake.portionRequests, request)
	fake.portionContexts = append(fake.portionContexts, ctx)
	if index < len(fake.portionErrs) && fake.portionErrs[index] != nil {
		return foodamount.Resolution{}, fake.portionErrs[index]
	}
	if index >= len(fake.portionResults) {
		return foodamount.Resolution{}, errors.New("unexpected portion selection")
	}
	return fake.portionResults[index], nil
}

type fakeFoodDetailer struct {
	detail   fooddetail.Detail
	err      error
	calls    int
	requests []fooddetail.Request
	contexts []context.Context
}

func (fake *fakeFoodDetailer) Get(ctx context.Context, request fooddetail.Request) (fooddetail.Detail, error) {
	fake.calls++
	fake.requests = append(fake.requests, request)
	fake.contexts = append(fake.contexts, ctx)
	return fake.detail, fake.err
}

type fakeNutritionCalculator struct {
	results  []nutritioncalc.Result
	errs     []error
	calls    int
	requests []nutritioncalc.Request
	contexts []context.Context
}

func (fake *fakeNutritionCalculator) Calculate(ctx context.Context, request nutritioncalc.Request) (nutritioncalc.Result, error) {
	index := fake.calls
	fake.calls++
	fake.requests = append(fake.requests, request)
	fake.contexts = append(fake.contexts, ctx)
	if index < len(fake.errs) && fake.errs[index] != nil {
		return nutritioncalc.Result{}, fake.errs[index]
	}
	if index < len(fake.results) {
		return fake.results[index], nil
	}
	resolvedGrams := 1.0
	if request.Grams != nil {
		resolvedGrams = *request.Grams
	}
	return nutritioncalc.Result{FoodID: request.FoodID, ResolvedGrams: resolvedGrams}, nil
}

func newTestService(extractor TextExtractor, resolver FoodResolver, amount AmountResolver) *Service {
	return NewService(extractor, resolver, amount, &fakeFoodDetailer{}, &fakeNutritionCalculator{})
}

func (fake *fakeAmountResolver) Resolve(ctx context.Context, request foodamount.Request) (foodamount.Resolution, error) {
	index := fake.calls
	fake.calls++
	fake.requests = append(fake.requests, request)
	fake.contexts = append(fake.contexts, ctx)
	if index < len(fake.errs) && fake.errs[index] != nil {
		return foodamount.Resolution{}, fake.errs[index]
	}
	return fake.results[index], nil
}

func TestInterpretTextValidatesLocaleBeforeExtraction(t *testing.T) {
	t.Parallel()

	extractor := &fakeTextExtractor{}
	result, err := newTestService(extractor, &fakeFoodResolver{}, &fakeAmountResolver{}).InterpretText(context.Background(), Request{
		Text: "elma", Locale: "tr_TR",
	})
	if !IsKind(err, ErrorInvalidInput) {
		t.Fatalf("error = %v, want invalid_input", err)
	}
	if extractor.calls != 0 {
		t.Fatalf("extractor calls = %d, want 0", extractor.calls)
	}
	assertZeroResult(t, result)
}

func TestInterpretTextEmptyExtraction(t *testing.T) {
	t.Parallel()

	extractor := &fakeTextExtractor{result: foodextraction.TextFoodExtraction{}}
	resolver := &fakeFoodResolver{}
	amount := &fakeAmountResolver{}
	result, err := newTestService(extractor, resolver, amount).InterpretText(context.Background(), Request{Text: "hiçbir şey yemedim"})
	if err != nil {
		t.Fatalf("InterpretText: %v", err)
	}
	if extractor.calls != 1 || resolver.calls != 0 || amount.calls != 0 {
		t.Fatalf("calls extractor/resolver/amount = %d/%d/%d", extractor.calls, resolver.calls, amount.calls)
	}
	if result.State != StateEmpty || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("result = %#v", result)
	}
	assertResultInvariants(t, result)
}

func TestInterpretTextMixedMealPreservesOrderAndContinuesAfterClarification(t *testing.T) {
	t.Parallel()

	quantityTwo, quantityGrams := 2.0, 200.0
	adet, grams := "adet", "g"
	firstIntent := foodintent.FoodIntent{Query: "yumurta", Quantity: &quantityTwo, UnitHint: &adet}
	secondIntent := foodintent.FoodIntent{Query: "tavuk", Quantity: &quantityGrams, UnitHint: &grams}
	extractor := &fakeTextExtractor{result: foodextraction.TextFoodExtraction{Items: []foodextraction.ExtractedTextFoodIntent{
		{Mention: "2 yumurta", Intent: firstIntent},
		{Mention: "200 g tavuk", Intent: secondIntent},
	}}}
	brandA, brandB := "A", "B"
	candidates := []foodsearch.FoodCandidate{
		{FoodID: 2, DisplayName: "İkinci", CanonicalName: "Second", Brand: &brandB},
		{FoodID: 1, DisplayName: "Birinci", CanonicalName: "First", Brand: &brandA},
	}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{
		{State: foodresolver.StateAmbiguous, Reason: foodresolver.ReasonMultipleExactIdentities, Candidates: candidates},
		resolvedIdentity(9, "Tavuk", "Chicken", nil),
	}}
	selection := &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 9, Grams: &foodamount.GramsSelection{Grams: 200}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{{
		State: foodamount.StateResolved, Reason: foodamount.ReasonExplicitGrams, Selection: selection,
	}}}
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "same")
	calculator := &fakeNutritionCalculator{}

	result, err := NewService(extractor, resolver, amount, &fakeFoodDetailer{}, calculator).InterpretText(ctx, Request{Text: "source", Locale: "TR-tr"})
	if err != nil {
		t.Fatalf("InterpretText: %v", err)
	}
	if result.State != StateClarificationRequired || len(result.Items) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Items[0].Mention != "2 yumurta" || result.Items[1].Mention != "200 g tavuk" {
		t.Fatalf("item order changed: %#v", result.Items)
	}
	if result.Items[0].State != ItemClarificationRequired || result.Items[1].State != ItemReady {
		t.Fatalf("item states = %s/%s", result.Items[0].State, result.Items[1].State)
	}
	if amount.calls != 1 || resolver.calls != 2 || extractor.calls != 1 {
		t.Fatalf("calls extractor/resolver/amount = %d/%d/%d", extractor.calls, resolver.calls, amount.calls)
	}
	if calculator.calls != 1 || result.Items[0].Preview != nil || result.Items[1].Preview == nil {
		t.Fatalf("calculator calls/previews = %d/%#v/%#v", calculator.calls, result.Items[0].Preview, result.Items[1].Preview)
	}
	if !reflect.DeepEqual(result.Items[0].Intent, firstIntent) || !reflect.DeepEqual(result.Items[1].Intent, secondIntent) {
		t.Fatal("validated intents were changed")
	}
	if result.Items[0].Clarification.Candidates[0].FoodID != 2 || result.Items[0].Clarification.Candidates[1].FoodID != 1 {
		t.Fatal("food candidate order changed")
	}
	if result.Items[0].Clarification.Portions == nil || result.Items[0].Clarification.AllowDirectGrams {
		t.Fatal("food clarification collections/flags are invalid")
	}
	if resolver.requests[0].Locale != "tr-TR" || resolver.requests[1].Locale != "tr-TR" || amount.requests[0].Locale != "tr-TR" {
		t.Fatalf("normalized locales were not forwarded: resolver=%#v amount=%#v", resolver.requests, amount.requests)
	}
	if !reflect.DeepEqual(resolver.requests[0].Intent, firstIntent) || !reflect.DeepEqual(amount.requests[0].Intent, secondIntent) {
		t.Fatal("downstream intents were changed")
	}
	if extractor.contexts[0] != ctx || resolver.contexts[0] != ctx || resolver.contexts[1] != ctx || amount.contexts[0] != ctx {
		t.Fatal("caller context was not forwarded unchanged")
	}
	if result.Items[1].Selection != selection {
		t.Fatal("trusted amount selection was not preserved")
	}
	assertResultInvariants(t, result)
}

func TestInterpretTextRepeatedMentionsRemainSeparateAndAllReady(t *testing.T) {
	t.Parallel()

	intent := foodintent.FoodIntent{Query: "yumurta", Quantity: floatPointer(2), UnitHint: stringPointer("g")}
	extractor := &fakeTextExtractor{result: foodextraction.TextFoodExtraction{Items: []foodextraction.ExtractedTextFoodIntent{
		{Mention: "yumurta", Intent: intent}, {Mention: "yumurta", Intent: intent},
	}}}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{
		resolvedIdentity(1, "Yumurta", "Egg", nil), resolvedIdentity(1, "Yumurta", "Egg", nil),
	}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{
		resolvedGrams(1, 2), resolvedGrams(1, 2),
	}}
	result, err := newTestService(extractor, resolver, amount).InterpretText(context.Background(), Request{Text: "yumurta yumurta", Locale: "tr"})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateReady || len(result.Items) != 2 || result.Items[0].Mention != "yumurta" || result.Items[1].Mention != "yumurta" {
		t.Fatalf("result = %#v", result)
	}
	if resolver.calls != 2 || amount.calls != 2 {
		t.Fatalf("resolver/amount calls = %d/%d", resolver.calls, amount.calls)
	}
	assertResultInvariants(t, result)
}

func TestInterpretTextCalculatesReadyPreviewsSequentiallyFromSelections(t *testing.T) {
	t.Parallel()

	extractor := twoItemExtractor()
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{
		resolvedIdentity(1, "First", "First", nil), resolvedIdentity(2, "Second", "Second", nil),
	}}
	portion := &foodamount.Selection{
		Kind: foodamount.SelectionPortion, FoodID: 2,
		Portion: &foodamount.PortionSelection{
			PortionID: 9, Quantity: 3, Amount: 1, Measure: "adet", PortionGrams: 40,
		},
	}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{
		resolvedGrams(1, 25),
		{State: foodamount.StateResolved, Reason: foodamount.ReasonExplicitPortionSelection, Selection: portion},
	}}
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{
		{FoodID: 1, ResolvedGrams: 25}, {FoodID: 2, ResolvedGrams: 119.75},
	}}
	result, err := NewService(extractor, resolver, amount, &fakeFoodDetailer{}, calculator).
		InterpretText(context.Background(), Request{Text: "meal", Locale: "tr"})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateReady || len(result.Items) != 2 || result.Items[0].Preview.ResolvedGrams != 25 || result.Items[1].Preview.ResolvedGrams != 119.75 {
		t.Fatalf("result = %#v", result)
	}
	if calculator.calls != 2 {
		t.Fatalf("calculator calls = %d", calculator.calls)
	}
	first, second := calculator.requests[0], calculator.requests[1]
	if first.FoodID != 1 || first.Grams == nil || *first.Grams != 25 || first.PortionID != nil || first.Quantity != nil {
		t.Fatalf("first nutrition request = %#v", first)
	}
	if second.FoodID != 2 || second.Grams != nil || second.PortionID == nil || *second.PortionID != 9 || second.Quantity == nil || *second.Quantity != 3 {
		t.Fatalf("second nutrition request = %#v", second)
	}
	assertResultInvariants(t, result)
}

func TestInterpretTextLaterNutritionFailureDiscardsAllItems(t *testing.T) {
	t.Parallel()

	cause := errors.New("nutrition storage failed")
	result, err := NewService(
		twoItemExtractor(),
		&fakeFoodResolver{results: []foodresolver.Resolution{
			resolvedIdentity(1, "First", "First", nil), resolvedIdentity(2, "Second", "Second", nil),
		}},
		&fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(1, 10), resolvedGrams(2, 20)}},
		&fakeFoodDetailer{},
		&fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: 1, ResolvedGrams: 10}}, errs: []error{nil, cause}},
	).InterpretText(context.Background(), Request{Text: "meal", Locale: "tr"})
	if !IsKind(err, ErrorResolutionFailure) || !errors.Is(err, cause) {
		t.Fatalf("error = %v", err)
	}
	assertZeroResult(t, result)
}

func TestInterpretTextNotFoundIsFoodClarificationWithoutAmountCall(t *testing.T) {
	t.Parallel()

	extractor := oneItemExtractor()
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{{
		State: foodresolver.StateNotFound, Reason: foodresolver.ReasonNoCandidates,
		Candidates: []foodsearch.FoodCandidate{},
	}}}
	amount := &fakeAmountResolver{}
	result, err := newTestService(extractor, resolver, amount).InterpretText(context.Background(), Request{Text: "elma", Locale: "tr"})
	if err != nil {
		t.Fatal(err)
	}
	item := result.Items[0]
	if item.Clarification.Reason != string(foodresolver.ReasonNoCandidates) || item.Clarification.Candidates == nil || len(item.Clarification.Candidates) != 0 || amount.calls != 0 {
		t.Fatalf("item/calls = %#v/%d", item, amount.calls)
	}
	assertResultInvariants(t, result)
}

func TestInterpretTextAmountClarificationPreservesPortions(t *testing.T) {
	t.Parallel()

	portions := []food.Portion{
		{ID: 2, FoodID: 8, Amount: 1, Measure: "second", Grams: 20},
		{ID: 1, FoodID: 8, Amount: 1, Measure: "first", Grams: 10},
	}
	amountClarification := &foodamount.Clarification{Portions: portions, AllowDirectGrams: true}
	calculator := &fakeNutritionCalculator{}
	result, err := NewService(
		oneItemExtractor(),
		&fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(8, "Elma", "Apple", nil)}},
		&fakeAmountResolver{results: []foodamount.Resolution{{
			State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonUnitRequired,
			Clarification: amountClarification,
		}}},
		&fakeFoodDetailer{}, calculator,
	).InterpretText(context.Background(), Request{Text: "elma", Locale: "tr"})
	if err != nil {
		t.Fatal(err)
	}
	item := result.Items[0]
	if item.Food == nil || item.Selection != nil || item.Clarification.Kind != ClarificationAmount || item.Clarification.Candidates == nil || len(item.Clarification.Candidates) != 0 || !reflect.DeepEqual(item.Clarification.Portions, portions) {
		t.Fatalf("item = %#v", item)
	}
	if calculator.calls != 0 {
		t.Fatalf("calculator calls = %d, want 0", calculator.calls)
	}
	assertResultInvariants(t, result)
}

func TestInterpretTextOperationalFailuresDiscardPartialResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		extractor *fakeTextExtractor
		resolver  *fakeFoodResolver
		amount    *fakeAmountResolver
		wantKind  ErrorKind
	}{
		{
			name: "extraction failure", extractor: &fakeTextExtractor{err: errors.New("provider")},
			resolver: &fakeFoodResolver{}, amount: &fakeAmountResolver{}, wantKind: ErrorAIFailure,
		},
		{
			name: "second resolver failure", extractor: twoItemExtractor(),
			resolver: &fakeFoodResolver{
				results: []foodresolver.Resolution{resolvedIdentity(1, "First", "First", nil)},
				errs:    []error{nil, &foodresolver.Error{Kind: foodresolver.ErrorSearchFailure}},
			},
			amount: &fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(1, 10)}}, wantKind: ErrorResolutionFailure,
		},
		{
			name: "second amount failure", extractor: twoItemExtractor(),
			resolver: &fakeFoodResolver{results: []foodresolver.Resolution{
				resolvedIdentity(1, "First", "First", nil), resolvedIdentity(2, "Second", "Second", nil),
			}},
			amount: &fakeAmountResolver{
				results: []foodamount.Resolution{resolvedGrams(1, 10)},
				errs:    []error{nil, &foodamount.Error{Kind: foodamount.ErrorDetailFailure}},
			},
			wantKind: ErrorResolutionFailure,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := newTestService(tt.extractor, tt.resolver, tt.amount).InterpretText(context.Background(), Request{Text: "meal", Locale: "tr"})
			if !IsKind(err, tt.wantKind) {
				t.Fatalf("error = %v, want %s", err, tt.wantKind)
			}
			assertZeroResult(t, result)
		})
	}
}

func TestInterpretTextRejectsMalformedFoodResolution(t *testing.T) {
	t.Parallel()

	candidate := foodsearch.FoodCandidate{FoodID: 1, DisplayName: "Food", CanonicalName: "Food"}
	tests := []foodresolver.Resolution{
		{State: foodresolver.StateResolved, Candidates: []foodsearch.FoodCandidate{candidate}},
		{State: foodresolver.StateResolved, Resolved: &candidate, Candidates: nil},
		{State: foodresolver.StateAmbiguous, Resolved: &candidate, Candidates: []foodsearch.FoodCandidate{candidate}},
		{State: foodresolver.StateAmbiguous, Candidates: []foodsearch.FoodCandidate{}},
		{State: foodresolver.StateNotFound, Candidates: []foodsearch.FoodCandidate{candidate}},
		{State: foodresolver.State("future"), Candidates: []foodsearch.FoodCandidate{}},
	}
	for _, malformed := range tests {
		resolver := &fakeFoodResolver{results: []foodresolver.Resolution{malformed}}
		result, err := newTestService(oneItemExtractor(), resolver, &fakeAmountResolver{}).InterpretText(context.Background(), Request{Text: "food"})
		if !IsKind(err, ErrorResolutionFailure) {
			t.Errorf("resolution %#v error = %v", malformed, err)
		}
		assertZeroResult(t, result)
	}
}

func TestInterpretTextRejectsMalformedAmountResolution(t *testing.T) {
	t.Parallel()

	validSelection := &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 1, Grams: &foodamount.GramsSelection{Grams: 10}}
	validClarification := &foodamount.Clarification{Portions: []food.Portion{}, AllowDirectGrams: true}
	tests := []foodamount.Resolution{
		{State: foodamount.StateResolved},
		{State: foodamount.StateResolved, Selection: validSelection, Clarification: validClarification},
		{State: foodamount.StateResolved, Selection: &foodamount.Selection{Kind: foodamount.SelectionKind("future"), FoodID: 1}},
		{State: foodamount.StateResolved, Selection: &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 2, Grams: &foodamount.GramsSelection{Grams: 10}}},
		{State: foodamount.StateResolved, Selection: &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 1, Grams: &foodamount.GramsSelection{Grams: math.NaN()}}},
		{State: foodamount.StateResolved, Selection: malformedPortionSelection(0, 1, "adet", 50)},
		{State: foodamount.StateResolved, Selection: malformedPortionSelection(1, 0, "adet", 50)},
		{State: foodamount.StateResolved, Selection: malformedPortionSelection(1, 1, "  ", 50)},
		{State: foodamount.StateResolved, Selection: malformedPortionSelection(1, 1, "adet", 0)},
		{State: foodamount.StateClarificationRequired},
		{State: foodamount.StateClarificationRequired, Selection: validSelection, Clarification: validClarification},
		{State: foodamount.StateClarificationRequired, Clarification: &foodamount.Clarification{Portions: nil, AllowDirectGrams: true}},
		{State: foodamount.State("future")},
	}
	for _, malformed := range tests {
		result, err := newTestService(
			oneItemExtractor(),
			&fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(1, "Food", "Food", nil)}},
			&fakeAmountResolver{results: []foodamount.Resolution{malformed}},
		).InterpretText(context.Background(), Request{Text: "food"})
		if !IsKind(err, ErrorResolutionFailure) {
			t.Errorf("resolution %#v error = %v", malformed, err)
		}
		assertZeroResult(t, result)
	}
}

func TestInterpretTextMapsDependencyErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		extract  error
		resolve  error
		amount   error
		wantKind ErrorKind
	}{
		{name: "extraction invalid", extract: foodextraction.NewError(foodextraction.ErrorInvalidInput, errors.New("detail")), wantKind: ErrorInvalidInput},
		{name: "provider unavailable", extract: foodextraction.NewError(foodextraction.ErrorProviderConfiguration, errors.New("detail")), wantKind: ErrorAIUnavailable},
		{name: "provider rate limit", extract: foodextraction.NewError(foodextraction.ErrorRateLimit, errors.New("detail")), wantKind: ErrorAIRateLimited},
		{name: "provider timeout", extract: foodextraction.NewError(foodextraction.ErrorTimeout, errors.New("detail")), wantKind: ErrorAITimeout},
		{name: "invalid provider response", extract: foodextraction.NewError(foodextraction.ErrorInvalidProviderOutput, errors.New("detail")), wantKind: ErrorAIInvalidResponse},
		{name: "provider failure", extract: foodextraction.NewError(foodextraction.ErrorProviderFailure, errors.New("detail")), wantKind: ErrorAIFailure},
		{name: "extraction canceled", extract: foodextraction.NewError(foodextraction.ErrorCanceled, context.Canceled), wantKind: ErrorCanceled},
		{name: "resolver invalid", resolve: &foodresolver.Error{Kind: foodresolver.ErrorInvalidInput}, wantKind: ErrorInvalidInput},
		{name: "resolver search failure", resolve: &foodresolver.Error{Kind: foodresolver.ErrorSearchFailure}, wantKind: ErrorResolutionFailure},
		{name: "resolver timeout", resolve: &foodresolver.Error{Kind: foodresolver.ErrorTimeout}, wantKind: ErrorTimeout},
		{name: "resolver canceled", resolve: &foodresolver.Error{Kind: foodresolver.ErrorCanceled}, wantKind: ErrorCanceled},
		{name: "amount invalid", amount: &foodamount.Error{Kind: foodamount.ErrorInvalidInput}, wantKind: ErrorInvalidInput},
		{name: "amount detail failure", amount: &foodamount.Error{Kind: foodamount.ErrorDetailFailure}, wantKind: ErrorResolutionFailure},
		{name: "amount food missing", amount: &foodamount.Error{Kind: foodamount.ErrorFoodNotFound}, wantKind: ErrorResolutionFailure},
		{name: "amount portion missing", amount: &foodamount.Error{Kind: foodamount.ErrorPortionNotFound}, wantKind: ErrorResolutionFailure},
		{name: "amount timeout", amount: &foodamount.Error{Kind: foodamount.ErrorTimeout}, wantKind: ErrorTimeout},
		{name: "amount canceled", amount: &foodamount.Error{Kind: foodamount.ErrorCanceled}, wantKind: ErrorCanceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			extractor := oneItemExtractor()
			resolver := &fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(1, "Food", "Food", nil)}}
			amount := &fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(1, 10)}}
			var cause error
			switch {
			case tt.extract != nil:
				extractor.err, cause = tt.extract, tt.extract
			case tt.resolve != nil:
				resolver.errs, cause = []error{tt.resolve}, tt.resolve
			case tt.amount != nil:
				amount.errs, cause = []error{tt.amount}, tt.amount
			}
			result, err := newTestService(extractor, resolver, amount).InterpretText(context.Background(), Request{Text: "food"})
			if !IsKind(err, tt.wantKind) {
				t.Fatalf("error = %v, want %s", err, tt.wantKind)
			}
			if !errors.Is(err, cause) {
				t.Fatalf("error does not preserve cause %v", cause)
			}
			assertZeroResult(t, result)
		})
	}
}

func TestInterpretTextMapsNutritionFailuresAndRejectsMalformedResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		result   nutritioncalc.Result
		err      error
		wantKind ErrorKind
	}{
		{name: "validation", err: &nutritioncalc.ValidationError{Field: "grams"}, wantKind: ErrorInvalidInput},
		{name: "operational", err: errors.New("storage"), wantKind: ErrorResolutionFailure},
		{name: "canceled", err: context.Canceled, wantKind: ErrorCanceled},
		{name: "timeout", err: context.DeadlineExceeded, wantKind: ErrorTimeout},
		{name: "food mismatch", result: nutritioncalc.Result{FoodID: 2, ResolvedGrams: 10}, wantKind: ErrorResolutionFailure},
		{name: "zero grams", result: nutritioncalc.Result{FoodID: 1, ResolvedGrams: 0}, wantKind: ErrorResolutionFailure},
		{name: "non-finite grams", result: nutritioncalc.Result{FoodID: 1, ResolvedGrams: math.Inf(1)}, wantKind: ErrorResolutionFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{test.result}}
			if test.err != nil {
				calculator.errs = []error{test.err}
			}
			result, err := NewService(
				oneItemExtractor(),
				&fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(1, "Food", "Food", nil)}},
				&fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(1, 10)}},
				&fakeFoodDetailer{}, calculator,
			).InterpretText(context.Background(), Request{Text: "food", Locale: "tr"})
			if !IsKind(err, test.wantKind) {
				t.Fatalf("error = %v, want %s", err, test.wantKind)
			}
			if test.err != nil && !errors.Is(err, test.err) {
				t.Fatalf("error does not preserve %v", test.err)
			}
			if calculator.calls != 1 {
				t.Fatalf("calculator calls = %d", calculator.calls)
			}
			assertZeroResult(t, result)
		})
	}
}

func assertResultInvariants(t *testing.T, result Result) {
	t.Helper()
	if result.Items == nil {
		t.Fatal("result items is nil")
	}
	switch result.State {
	case StateEmpty:
		if len(result.Items) != 0 {
			t.Fatalf("empty result has items: %#v", result)
		}
	case StateReady:
		if len(result.Items) == 0 {
			t.Fatal("ready result has no items")
		}
		for _, item := range result.Items {
			assertItemInvariants(t, item)
			if item.State != ItemReady {
				t.Fatal("ready result contains clarification")
			}
		}
	case StateClarificationRequired:
		clarificationCount := 0
		for _, item := range result.Items {
			assertItemInvariants(t, item)
			if item.State == ItemClarificationRequired {
				clarificationCount++
			}
		}
		if len(result.Items) == 0 || clarificationCount == 0 {
			t.Fatalf("invalid clarification result: %#v", result)
		}
	default:
		t.Fatalf("unknown result state %q", result.State)
	}
}

func assertItemInvariants(t *testing.T, item Item) {
	t.Helper()
	switch item.State {
	case ItemReady:
		if item.Food == nil || item.Selection == nil || item.Preview == nil || item.Clarification != nil {
			t.Fatalf("invalid ready item: %#v", item)
		}
	case ItemClarificationRequired:
		if item.Selection != nil || item.Preview != nil || item.Clarification == nil || item.Clarification.Candidates == nil || item.Clarification.Portions == nil {
			t.Fatalf("invalid clarification item: %#v", item)
		}
		if item.Clarification.Kind == ClarificationFoodIdentity {
			if item.Food != nil || len(item.Clarification.Portions) != 0 || item.Clarification.AllowDirectGrams {
				t.Fatalf("invalid food clarification: %#v", item)
			}
		} else if item.Clarification.Kind == ClarificationAmount {
			if item.Food == nil || len(item.Clarification.Candidates) != 0 || !item.Clarification.AllowDirectGrams {
				t.Fatalf("invalid amount clarification: %#v", item)
			}
		} else {
			t.Fatalf("unknown clarification kind %q", item.Clarification.Kind)
		}
	default:
		t.Fatalf("unknown item state %q", item.State)
	}
}

func assertZeroResult(t *testing.T, result Result) {
	t.Helper()
	if result.State != "" || result.Items != nil {
		t.Fatalf("result = %#v, want zero", result)
	}
}

func oneItemExtractor() *fakeTextExtractor {
	return &fakeTextExtractor{result: foodextraction.TextFoodExtraction{Items: []foodextraction.ExtractedTextFoodIntent{{
		Mention: "elma", Intent: foodintent.FoodIntent{Query: "elma"},
	}}}}
}

func twoItemExtractor() *fakeTextExtractor {
	return &fakeTextExtractor{result: foodextraction.TextFoodExtraction{Items: []foodextraction.ExtractedTextFoodIntent{
		{Mention: "first", Intent: foodintent.FoodIntent{Query: "first"}},
		{Mention: "second", Intent: foodintent.FoodIntent{Query: "second"}},
	}}}
}

func resolvedIdentity(id int64, display, canonical string, brand *string) foodresolver.Resolution {
	candidate := foodsearch.FoodCandidate{FoodID: id, DisplayName: display, CanonicalName: canonical, Brand: brand}
	return foodresolver.Resolution{
		State: foodresolver.StateResolved, Reason: foodresolver.ReasonUniqueExactIdentity,
		Resolved: &candidate, Candidates: []foodsearch.FoodCandidate{candidate},
	}
}

func resolvedGrams(foodID int64, grams float64) foodamount.Resolution {
	return foodamount.Resolution{
		State: foodamount.StateResolved, Reason: foodamount.ReasonExplicitGrams,
		Selection: &foodamount.Selection{
			Kind: foodamount.SelectionGrams, FoodID: foodID,
			Grams: &foodamount.GramsSelection{Grams: grams},
		},
	}
}

func malformedPortionSelection(quantity, amount float64, measure string, portionGrams float64) *foodamount.Selection {
	return &foodamount.Selection{
		Kind: foodamount.SelectionPortion, FoodID: 1,
		Portion: &foodamount.PortionSelection{
			PortionID: 9, Quantity: quantity, Amount: amount, Measure: measure, PortionGrams: portionGrams,
		},
	}
}

func floatPointer(value float64) *float64 { return &value }

func stringPointer(value string) *string { return &value }
