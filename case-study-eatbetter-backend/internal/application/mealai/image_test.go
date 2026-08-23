package mealai

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodimageextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodresolver"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type fakeImageExtractor struct {
	result   foodimageextraction.ImageFoodExtraction
	err      error
	calls    int
	inputs   []foodimageextraction.ImageInput
	contexts []context.Context
}

func (fake *fakeImageExtractor) Extract(ctx context.Context, input foodimageextraction.ImageInput) (foodimageextraction.ImageFoodExtraction, error) {
	fake.calls++
	fake.inputs = append(fake.inputs, input)
	fake.contexts = append(fake.contexts, ctx)
	return fake.result, fake.err
}

func TestInterpretImageValidatesLocaleAndMissingDependencySafely(t *testing.T) {
	t.Parallel()

	extractor := &fakeImageExtractor{}
	result, err := NewService(&fakeTextExtractor{}, extractor, nil, nil, nil, nil).InterpretImage(
		context.Background(), ImageRequest{Image: testImageInput(), Locale: "tr_TR"},
	)
	if !IsKind(err, ErrorInvalidInput) || extractor.calls != 0 || result.Items != nil {
		t.Fatalf("result/error/calls = %#v/%v/%d", result, err, extractor.calls)
	}

	services := []*Service{
		NewService(&fakeTextExtractor{}, nil, nil, nil, nil, nil),
		nil,
	}
	for _, service := range services {
		result, err := service.InterpretImage(context.Background(), ImageRequest{Image: testImageInput(), Locale: "tr"})
		if !IsKind(err, ErrorAIUnavailable) || result.Items != nil {
			t.Fatalf("result/error = %#v/%v, want ai_unavailable", result, err)
		}
	}
}

func TestInterpretImageEmptyExtraction(t *testing.T) {
	t.Parallel()

	extractor := &fakeImageExtractor{result: foodimageextraction.ImageFoodExtraction{Items: []foodimageextraction.ExtractedImageFoodIntent{}}}
	resolver, amount := &fakeFoodResolver{}, &fakeAmountResolver{}
	result, err := NewService(&fakeTextExtractor{}, extractor, resolver, amount, &fakeFoodDetailer{}, &fakeNutritionCalculator{}).
		InterpretImage(context.Background(), ImageRequest{Image: testImageInput()})
	if err != nil {
		t.Fatalf("InterpretImage: %v", err)
	}
	if result.State != StateEmpty || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if extractor.calls != 1 || resolver.calls != 0 || amount.calls != 0 {
		t.Fatalf("extractor/resolver/amount calls = %d/%d/%d", extractor.calls, resolver.calls, amount.calls)
	}
}

func TestInterpretImageRejectsMalformedExtractionBeforeResolution(t *testing.T) {
	t.Parallel()

	quantity := 1.0
	unit := "piece"
	validItem := foodimageextraction.ExtractedImageFoodIntent{
		Observation: "an apple",
		Intent:      foodintent.FoodIntent{Query: "apple"},
	}
	tooManyItems := make([]foodimageextraction.ExtractedImageFoodIntent, foodimageextraction.MaxItems+1)
	for index := range tooManyItems {
		tooManyItems[index] = validItem
	}
	tests := []struct {
		name  string
		items []foodimageextraction.ExtractedImageFoodIntent
	}{
		{name: "nil items", items: nil},
		{name: "too many items", items: tooManyItems},
		{name: "blank observation", items: []foodimageextraction.ExtractedImageFoodIntent{{Observation: "", Intent: validItem.Intent}}},
		{name: "observation whitespace", items: []foodimageextraction.ExtractedImageFoodIntent{{Observation: " an apple ", Intent: validItem.Intent}}},
		{name: "observation over rune limit", items: []foodimageextraction.ExtractedImageFoodIntent{{Observation: strings.Repeat("ö", foodimageextraction.MaxObservationRunes+1), Intent: validItem.Intent}}},
		{name: "invalid UTF-8 observation", items: []foodimageextraction.ExtractedImageFoodIntent{{Observation: string([]byte{0xff}), Intent: validItem.Intent}}},
		{name: "short query", items: []foodimageextraction.ExtractedImageFoodIntent{{Observation: validItem.Observation, Intent: foodintent.FoodIntent{Query: "x"}}}},
		{name: "query whitespace", items: []foodimageextraction.ExtractedImageFoodIntent{{Observation: validItem.Observation, Intent: foodintent.FoodIntent{Query: " apple "}}}},
		{name: "query over rune limit", items: []foodimageextraction.ExtractedImageFoodIntent{{Observation: validItem.Observation, Intent: foodintent.FoodIntent{Query: strings.Repeat("ö", foodimageextraction.MaxQueryRunes+1)}}}},
		{name: "invalid UTF-8 query", items: []foodimageextraction.ExtractedImageFoodIntent{{Observation: validItem.Observation, Intent: foodintent.FoodIntent{Query: string([]byte{0xff, 0xfe})}}}},
		{name: "quantity", items: []foodimageextraction.ExtractedImageFoodIntent{{Observation: validItem.Observation, Intent: foodintent.FoodIntent{Query: "apple", Quantity: &quantity}}}},
		{name: "unit", items: []foodimageextraction.ExtractedImageFoodIntent{{Observation: validItem.Observation, Intent: foodintent.FoodIntent{Query: "apple", UnitHint: &unit}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			extractor := &fakeImageExtractor{result: foodimageextraction.ImageFoodExtraction{Items: tt.items}}
			resolver := &fakeFoodResolver{}
			amount := &fakeAmountResolver{}
			calculator := &fakeNutritionCalculator{}
			result, err := NewService(
				&fakeTextExtractor{}, extractor, resolver, amount, &fakeFoodDetailer{}, calculator,
			).InterpretImage(context.Background(), ImageRequest{Image: testImageInput()})
			if !IsKind(err, ErrorAIInvalidResponse) || result.Items != nil {
				t.Fatalf("result/error = %#v/%v, want ai_invalid_response", result, err)
			}
			if extractor.calls != 1 || resolver.calls != 0 || amount.calls != 0 || calculator.calls != 0 {
				t.Fatalf("extractor/resolver/amount/nutrition calls = %d/%d/%d/%d", extractor.calls, resolver.calls, amount.calls, calculator.calls)
			}
		})
	}
}

func TestInterpretImagePreservesObservationIntentAndOrder(t *testing.T) {
	t.Parallel()

	firstIntent := foodintent.FoodIntent{Query: "red apple"}
	secondIntent := foodintent.FoodIntent{Query: "white rice"}
	extractor := &fakeImageExtractor{result: foodimageextraction.ImageFoodExtraction{Items: []foodimageextraction.ExtractedImageFoodIntent{
		{Observation: "a red apple", Intent: firstIntent},
		{Observation: "plain white rice", Intent: secondIntent},
	}}}
	candidate := foodsearch.FoodCandidate{FoodID: 3, DisplayName: "Elma", CanonicalName: "Apple"}
	resolver := &fakeFoodResolver{results: []foodresolver.Resolution{
		{State: foodresolver.StateAmbiguous, Reason: foodresolver.ReasonMultipleExactIdentities, Candidates: []foodsearch.FoodCandidate{candidate}},
		{State: foodresolver.StateNotFound, Reason: foodresolver.ReasonNoCandidates, Candidates: []foodsearch.FoodCandidate{}},
	}}
	amount := &fakeAmountResolver{}
	result, err := NewService(&fakeTextExtractor{}, extractor, resolver, amount, &fakeFoodDetailer{}, &fakeNutritionCalculator{}).
		InterpretImage(context.Background(), ImageRequest{Image: testImageInput(), Locale: "TR-tr"})
	if err != nil {
		t.Fatalf("InterpretImage: %v", err)
	}
	if result.State != StateClarificationRequired || len(result.Items) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Items[0].Observation != "a red apple" || result.Items[1].Observation != "plain white rice" {
		t.Fatalf("observation order changed: %#v", result.Items)
	}
	if !reflect.DeepEqual(result.Items[0].Intent, firstIntent) || !reflect.DeepEqual(result.Items[1].Intent, secondIntent) {
		t.Fatal("image intents changed")
	}
	if !reflect.DeepEqual(resolver.requests[0].Intent, firstIntent) || !reflect.DeepEqual(resolver.requests[1].Intent, secondIntent) || resolver.requests[0].Locale != "tr-TR" {
		t.Fatalf("resolver requests = %#v", resolver.requests)
	}
	if result.Items[0].Clarification.Kind != ClarificationFoodIdentity || result.Items[1].Clarification.Kind != ClarificationFoodIdentity || amount.calls != 0 {
		t.Fatalf("clarifications/amount calls = %#v/%d", result.Items, amount.calls)
	}
}

func TestInterpretImageResolvedIdentityRequiresAmountClarification(t *testing.T) {
	t.Parallel()

	intent := foodintent.FoodIntent{Query: "grilled chicken"}
	portions := []food.Portion{{ID: 9, FoodID: 7, Amount: 1, Measure: "piece", Grams: 100}}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{{
		State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired,
		Clarification: &foodamount.Clarification{Portions: portions, AllowDirectGrams: true},
	}}}
	calculator := &fakeNutritionCalculator{}
	result, err := NewService(
		&fakeTextExtractor{}, imageExtractor("visible grilled chicken", intent),
		&fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(7, "Tavuk", "Chicken", nil)}},
		amount, &fakeFoodDetailer{}, calculator,
	).InterpretImage(context.Background(), ImageRequest{Image: testImageInput(), Locale: "tr"})
	if err != nil {
		t.Fatal(err)
	}
	item := result.Items[0]
	if result.State != StateClarificationRequired || item.State != ItemClarificationRequired || item.Food == nil || item.Preview != nil || item.Selection != nil || item.Clarification == nil || item.Clarification.Kind != ClarificationAmount {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(item.Clarification.Portions, portions) || !item.Clarification.AllowDirectGrams || calculator.calls != 0 {
		t.Fatalf("clarification/calculator = %#v/%d", item.Clarification, calculator.calls)
	}
	if amount.requests[0].Intent.Quantity != nil || amount.requests[0].Intent.UnitHint != nil {
		t.Fatalf("image amount evidence was invented: %#v", amount.requests[0].Intent)
	}
}

func TestInterpretImageFullyResolvedUsesDeterministicNutrition(t *testing.T) {
	t.Parallel()

	intent := foodintent.FoodIntent{Query: "apple"}
	selection := resolvedGrams(4, 125)
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: 4, ResolvedGrams: 125}}}
	result, err := NewService(
		&fakeTextExtractor{}, imageExtractor("a red apple", intent),
		&fakeFoodResolver{results: []foodresolver.Resolution{resolvedIdentity(4, "Elma", "Apple", nil)}},
		&fakeAmountResolver{results: []foodamount.Resolution{selection}}, &fakeFoodDetailer{}, calculator,
	).InterpretImage(context.Background(), ImageRequest{Image: testImageInput()})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateReady || result.Items[0].Preview == nil || result.Items[0].Preview.ResolvedGrams != 125 || calculator.calls != 1 {
		t.Fatalf("result/calculator = %#v/%d", result, calculator.calls)
	}
	if calculator.requests[0].FoodID != 4 || calculator.requests[0].Grams == nil || *calculator.requests[0].Grams != 125 {
		t.Fatalf("nutrition request = %#v", calculator.requests[0])
	}
}

func TestInterpretImageMapsExtractionErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		kind ErrorKind
	}{
		{name: "invalid input", err: foodimageextraction.NewError(foodimageextraction.ErrorInvalidInput, errors.New("bad image")), kind: ErrorInvalidInput},
		{name: "configuration", err: foodimageextraction.NewError(foodimageextraction.ErrorProviderConfiguration, errors.New("missing")), kind: ErrorAIUnavailable},
		{name: "rate limit", err: foodimageextraction.NewError(foodimageextraction.ErrorRateLimit, errors.New("limited")), kind: ErrorAIRateLimited},
		{name: "timeout", err: foodimageextraction.NewError(foodimageextraction.ErrorTimeout, context.DeadlineExceeded), kind: ErrorAITimeout},
		{name: "invalid output", err: foodimageextraction.NewError(foodimageextraction.ErrorInvalidProviderOutput, errors.New("bad output")), kind: ErrorAIInvalidResponse},
		{name: "provider", err: foodimageextraction.NewError(foodimageextraction.ErrorProviderFailure, errors.New("failed")), kind: ErrorAIFailure},
		{name: "canceled", err: foodimageextraction.NewError(foodimageextraction.ErrorCanceled, context.Canceled), kind: ErrorCanceled},
		{name: "unknown", err: errors.New("unknown"), kind: ErrorAIFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			extractor := &fakeImageExtractor{err: tt.err}
			result, err := NewService(&fakeTextExtractor{}, extractor, nil, nil, nil, nil).
				InterpretImage(context.Background(), ImageRequest{Image: testImageInput()})
			if !IsKind(err, tt.kind) || extractor.calls != 1 || result.Items != nil {
				t.Fatalf("result/error/calls = %#v/%v/%d, want %s", result, err, extractor.calls, tt.kind)
			}
		})
	}
}

func TestInterpretImageRejectsMalformedFoodResolution(t *testing.T) {
	t.Parallel()

	intent := foodintent.FoodIntent{Query: "apple"}
	malformed := foodresolver.Resolution{State: foodresolver.StateResolved, Candidates: []foodsearch.FoodCandidate{}}
	result, err := NewService(
		&fakeTextExtractor{}, imageExtractor("an apple", intent),
		&fakeFoodResolver{results: []foodresolver.Resolution{malformed}}, &fakeAmountResolver{},
		&fakeFoodDetailer{}, &fakeNutritionCalculator{},
	).InterpretImage(context.Background(), ImageRequest{Image: testImageInput()})
	if !IsKind(err, ErrorResolutionFailure) || result.Items != nil {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
}

func imageExtractor(observation string, intent foodintent.FoodIntent) *fakeImageExtractor {
	return &fakeImageExtractor{result: foodimageextraction.ImageFoodExtraction{Items: []foodimageextraction.ExtractedImageFoodIntent{{
		Observation: observation, Intent: intent,
	}}}}
}

func testImageInput() foodimageextraction.ImageInput {
	return foodimageextraction.ImageInput{Data: []byte{0xff, 0xd8, 0xff}, MIMEType: "image/jpeg"}
}
