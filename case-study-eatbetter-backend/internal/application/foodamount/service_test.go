package foodamount

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type fakeDetailLoader struct {
	detail   fooddetail.Detail
	err      error
	calls    int
	requests []fooddetail.Request
	contexts []context.Context
}

func (fake *fakeDetailLoader) Get(ctx context.Context, request fooddetail.Request) (fooddetail.Detail, error) {
	fake.calls++
	fake.requests = append(fake.requests, request)
	fake.contexts = append(fake.contexts, ctx)
	return fake.detail, fake.err
}

func TestResolveDirectMass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		quantity   float64
		unit       string
		wantReason Reason
		wantGrams  float64
	}{
		{name: "explicit grams", quantity: 200, unit: "g", wantReason: ReasonExplicitGrams, wantGrams: 200},
		{name: "explicit kilograms", quantity: 0.5, unit: "kg", wantReason: ReasonExplicitKilograms, wantGrams: 500},
		{name: "safe classification normalization", quantity: 2, unit: " KG ", wantReason: ReasonExplicitKilograms, wantGrams: 2000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loader := &fakeDetailLoader{err: errors.New("must not be called")}
			unitBefore := tt.unit
			quantityBefore := tt.quantity
			resolution, err := NewService(loader).Resolve(context.Background(), Request{
				FoodID: 7,
				Intent: foodintent.FoodIntent{Query: "food", Quantity: &tt.quantity, UnitHint: &tt.unit},
				Locale: "tr-TR",
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if loader.calls != 0 {
				t.Fatalf("detail calls = %d, want 0", loader.calls)
			}
			if resolution.State != StateResolved || resolution.Reason != tt.wantReason {
				t.Fatalf("state/reason = %s/%s", resolution.State, resolution.Reason)
			}
			assertResolutionInvariants(t, resolution)
			assertGramsSelectionInvariants(t, resolution.Selection)
			if resolution.Selection.Grams.Grams != tt.wantGrams {
				t.Fatalf("grams = %v, want %v", resolution.Selection.Grams.Grams, tt.wantGrams)
			}
			if tt.unit != unitBefore || tt.quantity != quantityBefore {
				t.Fatal("FoodIntent amount was mutated")
			}
		})
	}
}

func TestResolveRejectsInvalidStructuralInputWithoutDetailCall(t *testing.T) {
	t.Parallel()

	zero, negative := 0.0, -1.0
	nan, positiveInfinity, negativeInfinity := math.NaN(), math.Inf(1), math.Inf(-1)
	maxFloat := math.MaxFloat64
	kg := "kg"
	g := "g"
	tests := []struct {
		name   string
		foodID int64
		intent foodintent.FoodIntent
	}{
		{name: "zero FoodID", foodID: 0, intent: foodintent.FoodIntent{Quantity: floatPointer(1), UnitHint: &g}},
		{name: "negative FoodID", foodID: -1, intent: foodintent.FoodIntent{Quantity: floatPointer(1), UnitHint: &g}},
		{name: "zero quantity", foodID: 1, intent: foodintent.FoodIntent{Quantity: &zero, UnitHint: &g}},
		{name: "negative quantity", foodID: 1, intent: foodintent.FoodIntent{Quantity: &negative, UnitHint: &g}},
		{name: "NaN quantity", foodID: 1, intent: foodintent.FoodIntent{Quantity: &nan, UnitHint: &g}},
		{name: "positive infinity", foodID: 1, intent: foodintent.FoodIntent{Quantity: &positiveInfinity, UnitHint: &g}},
		{name: "negative infinity", foodID: 1, intent: foodintent.FoodIntent{Quantity: &negativeInfinity, UnitHint: &g}},
		{name: "kilogram overflow", foodID: 1, intent: foodintent.FoodIntent{Quantity: &maxFloat, UnitHint: &kg}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loader := &fakeDetailLoader{}
			resolution, err := NewService(loader).Resolve(context.Background(), Request{FoodID: tt.foodID, Intent: tt.intent, Locale: "tr-TR"})
			if !IsKind(err, ErrorInvalidInput) {
				t.Fatalf("error = %v, want invalid_input", err)
			}
			assertZeroResolution(t, resolution)
			if loader.calls != 0 {
				t.Fatalf("detail calls = %d, want 0", loader.calls)
			}
		})
	}
}

func TestResolveDecisionPrecedence(t *testing.T) {
	t.Parallel()

	quantity := 2.0
	ml, unsupported, blank := "ml", "adet", "  "
	tests := []struct {
		name       string
		quantity   *float64
		unit       *string
		wantReason Reason
	}{
		{name: "quantity and unit missing", wantReason: ReasonQuantityRequired},
		{name: "quantity missing before volume", unit: &ml, wantReason: ReasonQuantityRequired},
		{name: "quantity missing before unsupported unit", unit: &unsupported, wantReason: ReasonQuantityRequired},
		{name: "unit nil", quantity: &quantity, wantReason: ReasonUnitRequired},
		{name: "unit blank", quantity: &quantity, unit: &blank, wantReason: ReasonUnitRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loader := &fakeDetailLoader{detail: fooddetail.Detail{Portions: []food.Portion{testPortion(1, 9, 1, "serving", 30)}}}
			resolution, err := NewService(loader).Resolve(context.Background(), Request{
				FoodID: 9, Intent: foodintent.FoodIntent{Quantity: tt.quantity, UnitHint: tt.unit}, Locale: "tr-TR",
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if resolution.State != StateClarificationRequired || resolution.Reason != tt.wantReason {
				t.Fatalf("state/reason = %s/%s", resolution.State, resolution.Reason)
			}
			if loader.calls != 1 {
				t.Fatalf("detail calls = %d, want 1", loader.calls)
			}
			assertResolutionInvariants(t, resolution)
		})
	}
}

func TestClarificationPreservesDetailStateAndCallerContext(t *testing.T) {
	t.Parallel()

	portions := []food.Portion{
		testPortion(8, 44, 2, "first", 25),
		testPortion(3, 44, 1, "second", 40),
	}
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "same")
	nutrition := &food.Nutrition{FoodID: 999}
	loader := &fakeDetailLoader{detail: fooddetail.Detail{Nutrition: nutrition, Portions: portions}}

	resolution, err := NewService(loader).Resolve(ctx, Request{FoodID: 44, Locale: "tr-Latn-TR"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if loader.calls != 1 || loader.contexts[0] != ctx {
		t.Fatalf("detail calls/context = %d/%v", loader.calls, loader.contexts[0] == ctx)
	}
	if loader.requests[0] != (fooddetail.Request{FoodID: 44, Locale: "tr-Latn-TR"}) {
		t.Fatalf("detail request = %#v", loader.requests[0])
	}
	if !reflect.DeepEqual(resolution.Clarification.Portions, portions) {
		t.Fatalf("portion order changed: got %#v want %#v", resolution.Clarification.Portions, portions)
	}
	assertResolutionInvariants(t, resolution)

	withoutNutrition := &fakeDetailLoader{detail: fooddetail.Detail{Portions: portions}}
	other, err := NewService(withoutNutrition).Resolve(ctx, Request{FoodID: 44, Locale: "tr-Latn-TR"})
	if err != nil {
		t.Fatalf("Resolve without nutrition: %v", err)
	}
	if !reflect.DeepEqual(resolution, other) {
		t.Fatalf("detail nutrition affected amount resolution: with=%#v without=%#v", resolution, other)
	}
}

func TestClarificationNormalizesNilPortionsToNonNilEmpty(t *testing.T) {
	t.Parallel()

	resolution, err := NewService(&fakeDetailLoader{}).Resolve(context.Background(), Request{FoodID: 1, Locale: "tr-TR"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolution.Clarification.Portions == nil || len(resolution.Clarification.Portions) != 0 {
		t.Fatalf("portions = %#v, want non-nil empty", resolution.Clarification.Portions)
	}
	if !resolution.Clarification.AllowDirectGrams {
		t.Fatal("AllowDirectGrams = false, want true")
	}
	assertResolutionInvariants(t, resolution)
}

func TestResolveVolumeAlwaysRequiresClarification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		quantity float64
		unit     string
	}{
		{quantity: 200, unit: "ml"},
		{quantity: 1, unit: "l"},
		{quantity: 1, unit: " ML "},
	}
	for _, tt := range tests {
		t.Run(tt.unit, func(t *testing.T) {
			t.Parallel()
			loader := &fakeDetailLoader{detail: fooddetail.Detail{Portions: []food.Portion{testPortion(1, 5, 200, "ml", 205)}}}
			resolution, err := NewService(loader).Resolve(context.Background(), Request{
				FoodID: 5, Intent: foodintent.FoodIntent{Quantity: &tt.quantity, UnitHint: &tt.unit}, Locale: "tr-TR",
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if resolution.Reason != ReasonVolumeRequiresClarification || resolution.Selection != nil {
				t.Fatalf("volume resolution = %#v", resolution)
			}
			if loader.calls != 1 {
				t.Fatalf("detail calls = %d, want 1", loader.calls)
			}
			assertResolutionInvariants(t, resolution)
		})
	}
}

func TestResolveUnsupportedUnitNeverAutoSelectsPortion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		quantity float64
		unit     string
		portions []food.Portion
	}{
		{name: "adet with one portion", quantity: 2, unit: "adet", portions: []food.Portion{testPortion(1, 6, 1, "adet", 50)}},
		{name: "matching dilim", quantity: 2, unit: "dilim", portions: []food.Portion{testPortion(2, 6, 1, "dilim", 30)}},
		{name: "quantity one with one portion", quantity: 1, unit: "kase", portions: []food.Portion{testPortion(3, 6, 1, "kase", 200)}},
		{name: "household unit", quantity: 1, unit: "bardak", portions: []food.Portion{testPortion(4, 6, 1, "bardak", 250)}},
		{name: "ounce", quantity: 4, unit: "oz", portions: []food.Portion{testPortion(5, 6, 1, "oz", 28)}},
		{name: "unsupported without portions", quantity: 3, unit: "slice", portions: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loader := &fakeDetailLoader{detail: fooddetail.Detail{Portions: tt.portions}}
			resolution, err := NewService(loader).Resolve(context.Background(), Request{
				FoodID: 6, Intent: foodintent.FoodIntent{Quantity: &tt.quantity, UnitHint: &tt.unit}, Locale: "tr-TR",
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if resolution.Reason != ReasonUnsupportedUnitRequiresClarification || resolution.Selection != nil {
				t.Fatalf("unsupported-unit resolution = %#v", resolution)
			}
			if loader.calls != 1 {
				t.Fatalf("detail calls = %d, want 1", loader.calls)
			}
			if resolution.Clarification.Portions == nil {
				t.Fatal("clarification portions is nil")
			}
			assertResolutionInvariants(t, resolution)
		})
	}
}

func TestResolveExplicitPortionSelectionUsesTrustedSnapshot(t *testing.T) {
	t.Parallel()

	selected := testPortion(22, 70, 1.5, "persisted measure", 35)
	loader := &fakeDetailLoader{detail: fooddetail.Detail{Portions: []food.Portion{
		testPortion(21, 70, 1, "other", 10), selected,
	}}}
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("selection"), "same")
	const userQuantity = 2.0

	resolution, err := NewService(loader).ResolvePortionSelection(ctx, PortionSelectionRequest{
		FoodID: 70, Locale: "tr-TR", PortionID: 22, Quantity: userQuantity,
	})
	if err != nil {
		t.Fatalf("ResolvePortionSelection: %v", err)
	}
	if loader.calls != 1 || loader.contexts[0] != ctx || loader.requests[0] != (fooddetail.Request{FoodID: 70, Locale: "tr-TR"}) {
		t.Fatalf("detail call not forwarded exactly once: calls=%d request=%#v", loader.calls, loader.requests[0])
	}
	if resolution.State != StateResolved || resolution.Reason != ReasonExplicitPortionSelection {
		t.Fatalf("state/reason = %s/%s", resolution.State, resolution.Reason)
	}
	assertResolutionInvariants(t, resolution)
	assertPortionSelectionInvariants(t, resolution.Selection)
	got := resolution.Selection.Portion
	if got.PortionID != selected.ID || got.Quantity != userQuantity || got.Amount != selected.Amount || got.Measure != selected.Measure || got.PortionGrams != selected.Grams {
		t.Fatalf("portion snapshot = %#v, selected = %#v", got, selected)
	}
	if got.PortionGrams != 35 {
		t.Fatalf("PortionGrams = %v; resolver appears to have multiplied by quantity", got.PortionGrams)
	}

	originalIntentQuantity := 2.0
	reusedLoader := &fakeDetailLoader{detail: loader.detail}
	reused, err := NewService(reusedLoader).ResolvePortionSelection(ctx, PortionSelectionRequest{
		FoodID: 70, Locale: "tr-TR", PortionID: 22, Quantity: originalIntentQuantity,
	})
	if err != nil || reused.Selection.Portion.Quantity != originalIntentQuantity {
		t.Fatalf("original explicit quantity was not reusable: resolution=%#v err=%v", reused, err)
	}
}

func TestResolvePortionSelectionRejectsInvalidInputWithoutDefaulting(t *testing.T) {
	t.Parallel()

	nan, infinity := math.NaN(), math.Inf(1)
	tests := []PortionSelectionRequest{
		{FoodID: 0, PortionID: 1, Quantity: 1},
		{FoodID: 1, PortionID: 0, Quantity: 1},
		{FoodID: 1, PortionID: -1, Quantity: 1},
		{FoodID: 1, PortionID: 1, Quantity: 0},
		{FoodID: 1, PortionID: 1, Quantity: -1},
		{FoodID: 1, PortionID: 1, Quantity: nan},
		{FoodID: 1, PortionID: 1, Quantity: infinity},
	}
	for _, request := range tests {
		loader := &fakeDetailLoader{}
		resolution, err := NewService(loader).ResolvePortionSelection(context.Background(), request)
		if !IsKind(err, ErrorInvalidInput) {
			t.Errorf("request %#v error = %v, want invalid_input", request, err)
		}
		assertZeroResolution(t, resolution)
		if loader.calls != 0 {
			t.Errorf("request %#v detail calls = %d, want 0", request, loader.calls)
		}
	}
}

func TestResolvePortionSelectionRequiresMatchingPersistedOwnership(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		portions []food.Portion
	}{
		{name: "missing portion", portions: []food.Portion{testPortion(8, 10, 1, "other", 20)}},
		{name: "portion belongs to another food", portions: []food.Portion{testPortion(9, 999, 1, "stale", 20)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loader := &fakeDetailLoader{detail: fooddetail.Detail{Portions: tt.portions}}
			resolution, err := NewService(loader).ResolvePortionSelection(context.Background(), PortionSelectionRequest{
				FoodID: 10, PortionID: 9, Quantity: 2, Locale: "tr-TR",
			})
			if !IsKind(err, ErrorPortionNotFound) {
				t.Fatalf("error = %v, want portion_not_found", err)
			}
			assertZeroResolution(t, resolution)
			if loader.calls != 1 {
				t.Fatalf("detail calls = %d, want 1", loader.calls)
			}
		})
	}
}

func TestDetailErrorsAreTypedAndReturnNoResolution(t *testing.T) {
	t.Parallel()

	validationCause := &fooddetail.ValidationError{Field: "locale"}
	operationalCause := errors.New("SQL implementation detail")
	tests := []struct {
		name      string
		err       error
		wantKind  ErrorKind
		wantCause error
	}{
		{name: "validation", err: fmt.Errorf("wrapped: %w", validationCause), wantKind: ErrorInvalidInput, wantCause: validationCause},
		{name: "food missing", err: fmt.Errorf("wrapped: %w", fooddetail.ErrNotFound), wantKind: ErrorFoodNotFound, wantCause: fooddetail.ErrNotFound},
		{name: "canceled", err: fmt.Errorf("wrapped: %w", context.Canceled), wantKind: ErrorCanceled, wantCause: context.Canceled},
		{name: "deadline", err: fmt.Errorf("wrapped: %w", context.DeadlineExceeded), wantKind: ErrorTimeout, wantCause: context.DeadlineExceeded},
		{name: "operational", err: operationalCause, wantKind: ErrorDetailFailure, wantCause: operationalCause},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loader := &fakeDetailLoader{err: tt.err}
			resolution, err := NewService(loader).Resolve(context.Background(), Request{FoodID: 1, Locale: "tr-TR"})
			if !IsKind(err, tt.wantKind) {
				t.Fatalf("error = %v, want %s", err, tt.wantKind)
			}
			if !errors.Is(err, tt.wantCause) {
				t.Fatalf("error = %v, want wrapped cause %v", err, tt.wantCause)
			}
			if strings.Contains(err.Error(), tt.wantCause.Error()) && err.Error() != string(tt.wantKind) {
				t.Fatalf("stable error exposed internal cause: %v", err)
			}
			assertZeroResolution(t, resolution)
			if loader.calls != 1 {
				t.Fatalf("detail calls = %d, want 1", loader.calls)
			}
		})
	}
}

func assertResolutionInvariants(t *testing.T, resolution Resolution) {
	t.Helper()
	switch resolution.State {
	case StateResolved:
		if resolution.Selection == nil || resolution.Clarification != nil {
			t.Fatalf("invalid resolved result: %#v", resolution)
		}
	case StateClarificationRequired:
		if resolution.Selection != nil || resolution.Clarification == nil || resolution.Clarification.Portions == nil || !resolution.Clarification.AllowDirectGrams {
			t.Fatalf("invalid clarification result: %#v", resolution)
		}
	default:
		t.Fatalf("unknown state %q", resolution.State)
	}
}

func assertGramsSelectionInvariants(t *testing.T, selection *Selection) {
	t.Helper()
	if selection.Kind != SelectionGrams || selection.FoodID <= 0 || selection.Grams == nil || selection.Portion != nil || !isFinitePositive(selection.Grams.Grams) {
		t.Fatalf("invalid grams selection: %#v", selection)
	}
}

func assertPortionSelectionInvariants(t *testing.T, selection *Selection) {
	t.Helper()
	if selection.Kind != SelectionPortion || selection.FoodID <= 0 || selection.Grams != nil || selection.Portion == nil || selection.Portion.PortionID <= 0 || !isFinitePositive(selection.Portion.Quantity) {
		t.Fatalf("invalid portion selection: %#v", selection)
	}
}

func assertZeroResolution(t *testing.T, resolution Resolution) {
	t.Helper()
	if resolution.State != "" || resolution.Reason != "" || resolution.Selection != nil || resolution.Clarification != nil {
		t.Fatalf("resolution = %#v, want zero result", resolution)
	}
}

func testPortion(id, foodID int64, amount float64, measure string, grams float64) food.Portion {
	return food.Portion{ID: id, FoodID: foodID, Amount: amount, Measure: measure, Grams: grams}
}

func floatPointer(value float64) *float64 { return &value }
