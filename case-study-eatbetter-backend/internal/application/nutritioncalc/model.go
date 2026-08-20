// Package nutritioncalc resolves trusted grams and calculates persisted canonical nutrition.
package nutritioncalc

import (
	"context"
	"errors"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

var (
	// ErrFoodNotFound means the supplied canonical food does not exist.
	ErrFoodNotFound = errors.New("food not found")
	// ErrPortionNotFound means the selected stored portion does not belong to the food.
	ErrPortionNotFound = errors.New("portion not found for food")
)

// Request supports exactly direct grams or one selected stored-portion multiplier.
// Pointers preserve JSON/application field presence independently from numeric zero.
type Request struct {
	FoodID    int64
	Grams     *float64
	PortionID *int64
	Quantity  *float64
}

// Source is the persisted deterministic input required for calculation.
type Source struct {
	Nutrition food.Nutrition
	Portion   *food.Portion
}

// Nutrition is the calculated nutrient amount for the resolved grams.
type Nutrition struct {
	Calories      food.NutrientAmount
	Protein       food.NutrientAmount
	Carbohydrates food.NutrientAmount
	Fat           food.NutrientAmount
}

// Result is sufficient for a client to render without performing nutrition math.
type Result struct {
	FoodID        int64
	ResolvedGrams float64
	Nutrition     Nutrition
}

// Repository loads canonical nutrition and, when requested, an owned trusted portion.
type Repository interface {
	Load(context.Context, int64, *int64) (Source, error)
}
