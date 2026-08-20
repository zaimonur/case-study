package nutritioncalc

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type stubRepository struct {
	source    Source
	err       error
	foodID    int64
	portionID *int64
}

func (r *stubRepository) Load(_ context.Context, foodID int64, portionID *int64) (Source, error) {
	r.foodID, r.portionID = foodID, portionID
	return r.source, r.err
}

func TestDirectGramsScaling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		grams    float64
		wantCals float64
	}{
		{"100g", 100, 123.45},
		{"50g", 50, 61.73},
		{"250g", 250, 308.63},
		{"fractional and rounded grams", 10.126, 12.51},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &stubRepository{source: Source{Nutrition: testNutrition(t, floatPointer(123.45), floatPointer(0), nil, floatPointer(2.5))}}
			got, err := NewService(repository).Calculate(context.Background(), Request{FoodID: 1, Grams: &test.grams})
			if err != nil {
				t.Fatal(err)
			}
			assertKnown(t, got.Nutrition.Calories, test.wantCals)
			assertKnown(t, got.Nutrition.Protein, 0)
			if got.Nutrition.Carbohydrates.IsKnown() {
				t.Fatal("missing carbohydrate became known")
			}
		})
	}
}

func TestStoredPortionQuantityMultipliesExactRecord(t *testing.T) {
	t.Parallel()
	portionID := int64(9)
	quantity := 2.0
	portion, err := food.NewPortion(1, 0.5, "cup, prepared", 120)
	if err != nil {
		t.Fatal(err)
	}
	portion.ID = portionID
	repository := &stubRepository{source: Source{Nutrition: testNutrition(t, floatPointer(100), nil, nil, nil), Portion: &portion}}
	got, err := NewService(repository).Calculate(context.Background(), Request{FoodID: 1, PortionID: &portionID, Quantity: &quantity})
	if err != nil {
		t.Fatal(err)
	}
	if got.ResolvedGrams != 240 {
		t.Fatalf("resolved grams = %v, want 240", got.ResolvedGrams)
	}
	assertKnown(t, got.Nutrition.Calories, 240)
}

func TestFractionalPortionQuantity(t *testing.T) {
	t.Parallel()
	portionID, quantity := int64(3), 0.5
	portion, _ := food.NewPortion(1, 1, "free form ml-looking text", 30)
	portion.ID = portionID
	got, err := NewService(&stubRepository{source: Source{Nutrition: testNutrition(t, floatPointer(100), nil, nil, nil), Portion: &portion}}).
		Calculate(context.Background(), Request{FoodID: 1, PortionID: &portionID, Quantity: &quantity})
	if err != nil || got.ResolvedGrams != 15 {
		t.Fatalf("result = %+v, error = %v", got, err)
	}
}

func TestValidationRejectsInvalidModesAndNumbers(t *testing.T) {
	t.Parallel()
	zero, negative, valid := 0.0, -1.0, 1.0
	zeroID := int64(0)
	tests := []Request{
		{},
		{FoodID: -1, Grams: &valid},
		{FoodID: 1, Grams: &zero},
		{FoodID: 1, Grams: &negative},
		{FoodID: 1, Grams: floatPointer(math.NaN())},
		{FoodID: 1, Grams: floatPointer(math.Inf(1))},
		{FoodID: 1, Grams: &valid, PortionID: &zeroID, Quantity: &valid},
		{FoodID: 1, PortionID: &zeroID, Quantity: &valid},
		{FoodID: 1, PortionID: int64Pointer(1)},
		{FoodID: 1, PortionID: int64Pointer(1), Quantity: &zero},
		{FoodID: 1, PortionID: int64Pointer(1), Quantity: &negative},
	}
	service := NewService(&stubRepository{})
	for _, request := range tests {
		if _, err := service.Calculate(context.Background(), request); !IsValidationError(err) {
			t.Errorf("Calculate(%+v) error = %v, want validation", request, err)
		}
	}
}

func TestMissingOrMismatchedPortionNeverFallsBack(t *testing.T) {
	t.Parallel()
	portionID, quantity := int64(2), 1.0
	request := Request{FoodID: 1, PortionID: &portionID, Quantity: &quantity}
	if _, err := NewService(&stubRepository{err: ErrPortionNotFound}).Calculate(context.Background(), request); !errors.Is(err, ErrPortionNotFound) {
		t.Fatalf("missing portion error = %v", err)
	}
	other, _ := food.NewPortion(99, 1, "slice", 20)
	other.ID = portionID
	if _, err := NewService(&stubRepository{source: Source{Nutrition: testNutrition(t, floatPointer(1), nil, nil, nil), Portion: &other}}).Calculate(context.Background(), request); !errors.Is(err, ErrPortionNotFound) {
		t.Fatalf("mismatched portion error = %v", err)
	}
}

func TestOverflowAndRoundingBoundary(t *testing.T) {
	t.Parallel()
	huge := math.MaxFloat64
	if _, err := NewService(&stubRepository{source: Source{Nutrition: testNutrition(t, floatPointer(1), nil, nil, nil)}}).
		Calculate(context.Background(), Request{FoodID: 1, Grams: &huge}); !IsValidationError(err) {
		t.Fatalf("huge grams error = %v, want validation", err)
	}
	grams := 1.005
	got, err := NewService(&stubRepository{source: Source{Nutrition: testNutrition(t, floatPointer(100), nil, nil, nil)}}).
		Calculate(context.Background(), Request{FoodID: 1, Grams: &grams})
	if err != nil || got.ResolvedGrams != 1 || nutrientValue(got.Nutrition.Calories) != 1 {
		t.Fatalf("rounding result = %+v, err = %v", got, err)
	}
}

func testNutrition(t *testing.T, values ...*float64) food.Nutrition {
	t.Helper()
	amounts := make([]food.NutrientAmount, 4)
	for index, value := range values {
		if value == nil {
			continue
		}
		var err error
		amounts[index], err = food.NewNutrientAmount(*value)
		if err != nil {
			t.Fatal(err)
		}
	}
	nutrition, err := food.NewNutrition(1, amounts[0], amounts[1], amounts[2], amounts[3])
	if err != nil {
		t.Fatal(err)
	}
	return nutrition
}

func assertKnown(t *testing.T, amount food.NutrientAmount, want float64) {
	t.Helper()
	got, known := amount.Value()
	if !known || got != want {
		t.Fatalf("amount = (%v, %v), want (%v, true)", got, known, want)
	}
}

func nutrientValue(amount food.NutrientAmount) float64 { value, _ := amount.Value(); return value }
func floatPointer(value float64) *float64              { return &value }
func int64Pointer(value int64) *int64                  { return &value }
