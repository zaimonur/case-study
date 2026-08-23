package mealai

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

func TestResolveSelectionDirectGramsPreservesIntentAndBuildsNutritionRequest(t *testing.T) {
	t.Parallel()

	originalQuantity, originalUnit, grams := 2.0, "adet", 120.0
	intent := foodintent.FoodIntent{Query: " yumurta ", Quantity: &originalQuantity, UnitHint: &originalUnit}
	detailer := &fakeFoodDetailer{detail: validDetail(7)}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(7, grams)}}
	zero, err := food.NewNutrientAmount(0)
	if err != nil {
		t.Fatal(err)
	}
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{
		FoodID: 7, ResolvedGrams: grams,
		Nutrition: nutritioncalc.Nutrition{Calories: zero},
	}}}
	extractor, resolver := &fakeTextExtractor{}, &fakeFoodResolver{}
	service := NewService(extractor, nil, resolver, amount, detailer, calculator)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "same")

	result, err := service.ResolveSelection(ctx, ResolveSelectionRequest{
		FoodID: 7, Locale: "TR-tr", Intent: intent,
		Choice: ExplicitChoice{Kind: ChoiceGrams, Grams: &grams},
	})
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	if !reflect.DeepEqual(result.Intent, intent) || result.State != ItemReady || result.Food == nil || result.Selection == nil || result.Preview == nil || result.Clarification != nil {
		t.Fatalf("result = %#v", result)
	}
	if amount.calls != 1 || amount.requests[0].Intent.Query != intent.Query || *amount.requests[0].Intent.Quantity != grams || *amount.requests[0].Intent.UnitHint != "g" {
		t.Fatalf("amount requests = %#v", amount.requests)
	}
	if calculator.calls != 1 || calculator.requests[0].FoodID != 7 || calculator.requests[0].Grams == nil || *calculator.requests[0].Grams != grams || calculator.requests[0].PortionID != nil || calculator.requests[0].Quantity != nil {
		t.Fatalf("nutrition requests = %#v", calculator.requests)
	}
	if detailer.calls != 1 || detailer.requests[0] != (fooddetail.Request{FoodID: 7, Locale: "tr-TR"}) {
		t.Fatalf("detail requests = %#v", detailer.requests)
	}
	if extractor.calls != 0 || resolver.calls != 0 || detailer.contexts[0] != ctx || amount.contexts[0] != ctx || calculator.contexts[0] != ctx {
		t.Fatal("continuation called forbidden dependency or changed context")
	}
	if value, known := result.Preview.Nutrition.Calories.Value(); !known || value != 0 {
		t.Fatalf("known zero calories = (%v, %v)", value, known)
	}
	if _, known := result.Preview.Nutrition.Protein.Value(); known {
		t.Fatal("unknown protein became known")
	}
}

func TestResolveSelectionPortionUsesTrustedSelectionWithoutLocalMath(t *testing.T) {
	t.Parallel()

	portionID, quantity := int64(9), 2.5
	selection := &foodamount.Selection{
		Kind: foodamount.SelectionPortion, FoodID: 7,
		Portion: &foodamount.PortionSelection{
			PortionID: portionID, Quantity: quantity, Amount: 1, Measure: "adet", PortionGrams: 40,
		},
	}
	amount := &fakeAmountResolver{portionResults: []foodamount.Resolution{{
		State: foodamount.StateResolved, Reason: foodamount.ReasonExplicitPortionSelection, Selection: selection,
	}}}
	calculator := &fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: 7, ResolvedGrams: 83.25}}}
	service := NewService(&fakeTextExtractor{}, nil, &fakeFoodResolver{}, amount, &fakeFoodDetailer{detail: validDetail(7)}, calculator)

	result, err := service.ResolveSelection(context.Background(), ResolveSelectionRequest{
		FoodID: 7, Locale: "tr", Intent: foodintent.FoodIntent{Query: "elma"},
		Choice: ExplicitChoice{Kind: ChoicePortion, PortionID: &portionID, Quantity: &quantity},
	})
	if err != nil {
		t.Fatal(err)
	}
	if amount.calls != 0 || amount.portionCalls != 1 || amount.portionRequests[0] != (foodamount.PortionSelectionRequest{FoodID: 7, Locale: "tr", PortionID: 9, Quantity: 2.5}) {
		t.Fatalf("amount calls/requests = %d/%d/%#v", amount.calls, amount.portionCalls, amount.portionRequests)
	}
	request := calculator.requests[0]
	if request.Grams != nil || request.PortionID == nil || *request.PortionID != portionID || request.Quantity == nil || *request.Quantity != quantity {
		t.Fatalf("nutrition request = %#v", request)
	}
	if result.Preview.ResolvedGrams != 83.25 {
		t.Fatalf("resolved grams = %v, want calculator value 83.25", result.Preview.ResolvedGrams)
	}
}

func TestResolveSelectionFoodIdentityCanReturnAmountClarification(t *testing.T) {
	t.Parallel()

	intent := foodintent.FoodIntent{Query: "elma"}
	amount := &fakeAmountResolver{results: []foodamount.Resolution{{
		State: foodamount.StateClarificationRequired, Reason: foodamount.ReasonQuantityRequired,
		Clarification: &foodamount.Clarification{Portions: []food.Portion{}, AllowDirectGrams: true},
	}}}
	calculator := &fakeNutritionCalculator{}
	result, err := NewService(
		&fakeTextExtractor{}, nil,
		&fakeFoodResolver{}, amount,
		&fakeFoodDetailer{detail: validDetail(7)}, calculator).
		ResolveSelection(context.Background(), ResolveSelectionRequest{
			FoodID: 7, Locale: "tr", Intent: intent, Choice: ExplicitChoice{Kind: ChoiceFoodIdentity},
		})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != ItemClarificationRequired || result.Food == nil || result.Selection != nil || result.Preview != nil || result.Clarification == nil || result.Clarification.Kind != ClarificationAmount {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(amount.requests[0].Intent, intent) || calculator.calls != 0 {
		t.Fatal("food identity intent changed or nutrition was calculated")
	}
}

func TestResolveSelectionRejectsLocalInputBeforeDependencies(t *testing.T) {
	t.Parallel()

	grams, portionID, quantity := 10.0, int64(2), 1.0
	longUnit := "123456789012345678901234567890123"
	nan := math.NaN()
	tests := []ResolveSelectionRequest{
		{FoodID: 0, Locale: "tr", Intent: foodintent.FoodIntent{Query: "elma"}, Choice: ExplicitChoice{Kind: ChoiceFoodIdentity}},
		{FoodID: 1, Locale: "tr_TR", Intent: foodintent.FoodIntent{Query: "elma"}, Choice: ExplicitChoice{Kind: ChoiceFoodIdentity}},
		{FoodID: 1, Locale: "tr", Intent: foodintent.FoodIntent{Query: "x"}, Choice: ExplicitChoice{Kind: ChoiceFoodIdentity}},
		{FoodID: 1, Locale: "tr", Intent: foodintent.FoodIntent{Query: "elma", Quantity: &nan}, Choice: ExplicitChoice{Kind: ChoiceFoodIdentity}},
		{FoodID: 1, Locale: "tr", Intent: foodintent.FoodIntent{Query: "elma", UnitHint: &longUnit}, Choice: ExplicitChoice{Kind: ChoiceFoodIdentity}},
		{FoodID: 1, Locale: "tr", Intent: foodintent.FoodIntent{Query: "elma"}, Choice: ExplicitChoice{Kind: ChoiceFoodIdentity, Grams: &grams}},
		{FoodID: 1, Locale: "tr", Intent: foodintent.FoodIntent{Query: "elma"}, Choice: ExplicitChoice{Kind: ChoiceGrams, Grams: &grams, Quantity: &quantity}},
		{FoodID: 1, Locale: "tr", Intent: foodintent.FoodIntent{Query: "elma"}, Choice: ExplicitChoice{Kind: ChoicePortion, PortionID: &portionID}},
		{FoodID: 1, Locale: "tr", Intent: foodintent.FoodIntent{Query: "elma"}, Choice: ExplicitChoice{Kind: ChoiceKind("future")}},
	}
	for _, request := range tests {
		detailer, amount, calculator := &fakeFoodDetailer{}, &fakeAmountResolver{}, &fakeNutritionCalculator{}
		result, err := NewService(&fakeTextExtractor{}, nil, &fakeFoodResolver{}, amount, detailer, calculator).ResolveSelection(context.Background(), request)
		if !IsKind(err, ErrorInvalidInput) {
			t.Errorf("request %#v error = %v", request, err)
		}
		if result != (ResolveSelectionResult{}) || detailer.calls != 0 || amount.calls != 0 || amount.portionCalls != 0 || calculator.calls != 0 {
			t.Errorf("invalid request reached dependency: result=%#v", result)
		}
	}
}

func TestResolveSelectionMapsNotFoundAndDiscardsPartialResult(t *testing.T) {
	t.Parallel()

	request := ResolveSelectionRequest{
		FoodID: 7, Locale: "tr", Intent: foodintent.FoodIntent{Query: "elma"}, Choice: ExplicitChoice{Kind: ChoiceFoodIdentity},
	}
	t.Run("food detail", func(t *testing.T) {
		result, err := NewService(
			&fakeTextExtractor{}, nil,
			&fakeFoodResolver{}, &fakeAmountResolver{},
			&fakeFoodDetailer{err: fooddetail.ErrNotFound}, &fakeNutritionCalculator{}).
			ResolveSelection(context.Background(), request)
		if !IsKind(err, ErrorFoodNotFound) || result != (ResolveSelectionResult{}) {
			t.Fatalf("result/error = %#v/%v", result, err)
		}
	})
	t.Run("portion", func(t *testing.T) {
		portionID, quantity := int64(1), 1.0
		request.Choice = ExplicitChoice{Kind: ChoicePortion, PortionID: &portionID, Quantity: &quantity}
		result, err := NewService(
			&fakeTextExtractor{}, nil,
			&fakeFoodResolver{},
			&fakeAmountResolver{portionErrs: []error{&foodamount.Error{Kind: foodamount.ErrorPortionNotFound}}},
			&fakeFoodDetailer{detail: validDetail(7)}, &fakeNutritionCalculator{}).
			ResolveSelection(context.Background(), request)
		if !IsKind(err, ErrorPortionNotFound) || result != (ResolveSelectionResult{}) {
			t.Fatalf("result/error = %#v/%v", result, err)
		}
	})
}

func TestResolveSelectionRejectsMalformedDependencySuccess(t *testing.T) {
	t.Parallel()

	grams := 10.0
	request := ResolveSelectionRequest{
		FoodID: 7, Locale: "tr", Intent: foodintent.FoodIntent{Query: "elma"}, Choice: ExplicitChoice{Kind: ChoiceGrams, Grams: &grams},
	}
	t.Run("food detail identity mismatch", func(t *testing.T) {
		result, err := NewService(
			&fakeTextExtractor{}, nil,
			&fakeFoodResolver{}, &fakeAmountResolver{},
			&fakeFoodDetailer{detail: validDetail(8)}, &fakeNutritionCalculator{}).
			ResolveSelection(context.Background(), request)
		if !IsKind(err, ErrorResolutionFailure) || result != (ResolveSelectionResult{}) {
			t.Fatalf("result/error = %#v/%v", result, err)
		}
	})
	t.Run("nutrition food mismatch", func(t *testing.T) {
		result, err := NewService(
			&fakeTextExtractor{}, nil,
			&fakeFoodResolver{}, &fakeAmountResolver{results: []foodamount.Resolution{resolvedGrams(7, grams)}},
			&fakeFoodDetailer{detail: validDetail(7)},
			&fakeNutritionCalculator{results: []nutritioncalc.Result{{FoodID: 8, ResolvedGrams: grams}}}).
			ResolveSelection(context.Background(), request)
		if !IsKind(err, ErrorResolutionFailure) || result != (ResolveSelectionResult{}) {
			t.Fatalf("result/error = %#v/%v", result, err)
		}
	})
}

func TestNutritionErrorMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		kind ErrorKind
	}{
		{err: &nutritioncalc.ValidationError{Field: "grams"}, kind: ErrorInvalidInput},
		{err: context.Canceled, kind: ErrorCanceled},
		{err: context.DeadlineExceeded, kind: ErrorTimeout},
		{err: nutritioncalc.ErrFoodNotFound, kind: ErrorResolutionFailure},
		{err: errors.New("storage"), kind: ErrorResolutionFailure},
	}
	for _, test := range tests {
		mapped := mapNutritionError(test.err)
		if !IsKind(mapped, test.kind) || !errors.Is(mapped, test.err) {
			t.Errorf("mapNutritionError(%v) = %v, want %s", test.err, mapped, test.kind)
		}
	}
}

func validDetail(foodID int64) fooddetail.Detail {
	return fooddetail.Detail{
		Food: food.Food{ID: foodID, CanonicalName: "Apple"}, DisplayName: "Elma", Portions: []food.Portion{},
	}
}

func TestResolveSelectionRejectsMalformedPersistedFoodDetailBeforeAmount(t *testing.T) {
	t.Parallel()

	grams := 10.0
	request := ResolveSelectionRequest{
		FoodID: 7,
		Locale: "tr",
		Intent: foodintent.FoodIntent{Query: "elma"},
		Choice: ExplicitChoice{Kind: ChoiceGrams, Grams: &grams},
	}
	blankBrand := "   "

	tests := []struct {
		name   string
		mutate func(*fooddetail.Detail)
	}{
		{
			name: "non-positive food id",
			mutate: func(detail *fooddetail.Detail) {
				detail.Food.ID = 0
			},
		},
		{
			name: "blank canonical name",
			mutate: func(detail *fooddetail.Detail) {
				detail.Food.CanonicalName = "   "
			},
		},
		{
			name: "blank display name",
			mutate: func(detail *fooddetail.Detail) {
				detail.DisplayName = "   "
			},
		},
		{
			name: "blank non-nil brand",
			mutate: func(detail *fooddetail.Detail) {
				detail.Food.Brand = &blankBrand
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			detail := validDetail(7)
			tt.mutate(&detail)

			detailer := &fakeFoodDetailer{detail: detail}
			amount := &fakeAmountResolver{
				results: []foodamount.Resolution{resolvedGrams(7, grams)},
			}
			calculator := &fakeNutritionCalculator{}

			result, err := NewService(
				&fakeTextExtractor{}, nil,

				&fakeFoodResolver{},
				amount,
				detailer,
				calculator).
				ResolveSelection(context.Background(), request)

			if !IsKind(err, ErrorResolutionFailure) {
				t.Fatalf("error = %v, want resolution_failure", err)
			}
			if result != (ResolveSelectionResult{}) {
				t.Fatalf("result = %#v, want zero", result)
			}
			if detailer.calls != 1 {
				t.Fatalf("detail calls = %d, want 1", detailer.calls)
			}
			if amount.calls != 0 || amount.portionCalls != 0 || calculator.calls != 0 {
				t.Fatalf(
					"malformed detail reached downstream dependencies: amount=%d portion=%d calculator=%d",
					amount.calls,
					amount.portionCalls,
					calculator.calls,
				)
			}
		})
	}
}

func TestResolveSelectionRejectsExplicitChoiceAmountShapeMismatch(t *testing.T) {
	t.Parallel()

	grams := 100.0
	portionID := int64(9)
	quantity := 2.0

	clarification := foodamount.Resolution{
		State:  foodamount.StateClarificationRequired,
		Reason: foodamount.ReasonUnitRequired,
		Clarification: &foodamount.Clarification{
			Portions:         []food.Portion{},
			AllowDirectGrams: true,
		},
	}

	portionResolution := foodamount.Resolution{
		State:  foodamount.StateResolved,
		Reason: foodamount.ReasonExplicitPortionSelection,
		Selection: &foodamount.Selection{
			Kind:   foodamount.SelectionPortion,
			FoodID: 7,
			Portion: &foodamount.PortionSelection{
				PortionID:    portionID,
				Quantity:     quantity,
				Amount:       1,
				Measure:      "large",
				PortionGrams: 50,
			},
		},
	}

	tests := []struct {
		name       string
		choice     ExplicitChoice
		resolution foodamount.Resolution
		viaPortion bool
	}{
		{
			name:       "grams choice returned clarification",
			choice:     ExplicitChoice{Kind: ChoiceGrams, Grams: &grams},
			resolution: clarification,
		},
		{
			name:       "grams choice returned portion",
			choice:     ExplicitChoice{Kind: ChoiceGrams, Grams: &grams},
			resolution: portionResolution,
		},
		{
			name: "portion choice returned clarification",
			choice: ExplicitChoice{
				Kind: ChoicePortion, PortionID: &portionID, Quantity: &quantity,
			},
			resolution: clarification,
			viaPortion: true,
		},
		{
			name: "portion choice returned grams",
			choice: ExplicitChoice{
				Kind: ChoicePortion, PortionID: &portionID, Quantity: &quantity,
			},
			resolution: resolvedGrams(7, grams),
			viaPortion: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			amount := &fakeAmountResolver{}
			if tt.viaPortion {
				amount.portionResults = []foodamount.Resolution{tt.resolution}
			} else {
				amount.results = []foodamount.Resolution{tt.resolution}
			}

			calculator := &fakeNutritionCalculator{}

			result, err := NewService(
				&fakeTextExtractor{}, nil,

				&fakeFoodResolver{},
				amount,
				&fakeFoodDetailer{detail: validDetail(7)},
				calculator).
				ResolveSelection(context.Background(), ResolveSelectionRequest{
					FoodID: 7,
					Locale: "tr",
					Intent: foodintent.FoodIntent{Query: "elma"},
					Choice: tt.choice,
				})

			if !IsKind(err, ErrorResolutionFailure) {
				t.Fatalf("error = %v, want resolution_failure", err)
			}
			if result != (ResolveSelectionResult{}) {
				t.Fatalf("result = %#v, want zero", result)
			}
			if calculator.calls != 0 {
				t.Fatalf("calculator calls = %d, want 0", calculator.calls)
			}

			if tt.viaPortion {
				if amount.portionCalls != 1 || amount.calls != 0 {
					t.Fatalf(
						"amount calls = %d portion calls = %d",
						amount.calls,
						amount.portionCalls,
					)
				}
			} else if amount.calls != 1 || amount.portionCalls != 0 {
				t.Fatalf(
					"amount calls = %d portion calls = %d",
					amount.calls,
					amount.portionCalls,
				)
			}
		})
	}
}

func TestResolveSelectionPortionChoiceQuantityOverridesIntentQuantity(t *testing.T) {
	t.Parallel()

	intentQuantity := 2.0
	choiceQuantity := 3.0
	portionID := int64(53985)

	intent := foodintent.FoodIntent{
		Query:    "yumurta",
		Quantity: &intentQuantity,
		UnitHint: stringPointer("adet"),
	}

	amount := &fakeAmountResolver{
		portionResults: []foodamount.Resolution{{
			State:  foodamount.StateResolved,
			Reason: foodamount.ReasonExplicitPortionSelection,
			Selection: &foodamount.Selection{
				Kind:   foodamount.SelectionPortion,
				FoodID: 7,
				Portion: &foodamount.PortionSelection{
					PortionID:    portionID,
					Quantity:     choiceQuantity,
					Amount:       1,
					Measure:      "large",
					PortionGrams: 50,
				},
			},
		}},
	}

	calculator := &fakeNutritionCalculator{
		results: []nutritioncalc.Result{{
			FoodID:        7,
			ResolvedGrams: 150,
		}},
	}

	result, err := NewService(
		&fakeTextExtractor{}, nil,

		&fakeFoodResolver{},
		amount,
		&fakeFoodDetailer{detail: validDetail(7)},
		calculator).
		ResolveSelection(context.Background(), ResolveSelectionRequest{
			FoodID: 7,
			Locale: "tr",
			Intent: intent,
			Choice: ExplicitChoice{
				Kind:      ChoicePortion,
				PortionID: &portionID,
				Quantity:  &choiceQuantity,
			},
		})
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}

	if result.Intent.Quantity == nil || *result.Intent.Quantity != 2 {
		t.Fatalf("original intent quantity changed: %#v", result.Intent.Quantity)
	}
	if amount.portionCalls != 1 ||
		amount.portionRequests[0].Quantity != 3 {
		t.Fatalf("portion request = %#v", amount.portionRequests)
	}

	request := calculator.requests[0]
	if request.Quantity == nil || *request.Quantity != 3 ||
		request.PortionID == nil || *request.PortionID != portionID {
		t.Fatalf("nutrition request = %#v", request)
	}

	if result.Selection == nil ||
		result.Selection.Portion == nil ||
		result.Selection.Portion.Quantity != 3 {
		t.Fatalf("selection = %#v", result.Selection)
	}
	if result.Preview == nil || result.Preview.ResolvedGrams != 150 {
		t.Fatalf("preview = %#v", result.Preview)
	}
}
