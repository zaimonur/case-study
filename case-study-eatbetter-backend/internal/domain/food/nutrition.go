package food

import (
	"fmt"
	"math"
)

// NutrientAmount preserves the distinction between an unavailable nutrient and a known value.
// Its zero value represents an unavailable nutrient.
type NutrientAmount struct {
	value float64
	known bool
}

// NewNutrientAmount creates a known, non-negative nutrient amount.
func NewNutrientAmount(value float64) (NutrientAmount, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return NutrientAmount{}, fmt.Errorf("nutrient amount must be a finite non-negative number")
	}

	return NutrientAmount{value: value, known: true}, nil
}

// IsKnown reports whether the nutrient value is available.
func (a NutrientAmount) IsKnown() bool {
	return a.known
}

// Value returns the amount and whether it is available.
func (a NutrientAmount) Value() (float64, bool) {
	return a.value, a.known
}

// Nutrition contains the canonical per-100-gram MVP nutrient values for a food.
type Nutrition struct {
	FoodID               int64
	CaloriesPer100g      NutrientAmount
	ProteinPer100g       NutrientAmount
	CarbohydratesPer100g NutrientAmount
	FatPer100g           NutrientAmount
}

// NewNutrition creates nutrition when at least one canonical nutrient is known.
func NewNutrition(
	foodID int64,
	caloriesPer100g NutrientAmount,
	proteinPer100g NutrientAmount,
	carbohydratesPer100g NutrientAmount,
	fatPer100g NutrientAmount,
) (Nutrition, error) {
	if !caloriesPer100g.IsKnown() &&
		!proteinPer100g.IsKnown() &&
		!carbohydratesPer100g.IsKnown() &&
		!fatPer100g.IsKnown() {
		return Nutrition{}, fmt.Errorf("at least one nutrient amount must be known")
	}

	return Nutrition{
		FoodID:               foodID,
		CaloriesPer100g:      caloriesPer100g,
		ProteinPer100g:       proteinPer100g,
		CarbohydratesPer100g: carbohydratesPer100g,
		FatPer100g:           fatPer100g,
	}, nil
}
