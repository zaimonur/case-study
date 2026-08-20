package nutritioncalc

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

// ValidationError identifies an invalid deterministic calculation command.
type ValidationError struct{ Field string }

func (e *ValidationError) Error() string { return fmt.Sprintf("invalid %s", e.Field) }

// IsValidationError reports whether an error is safe to map to invalid_request.
func IsValidationError(err error) bool {
	var validationError *ValidationError
	return errors.As(err, &validationError)
}

// Service owns portion resolution and pure nutrition scaling.
type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

// Calculate resolves grams, rounds them to two decimals, then scales every known nutrient.
func (s *Service) Calculate(ctx context.Context, request Request) (Result, error) {
	if request.FoodID <= 0 {
		return Result{}, &ValidationError{Field: "food_id"}
	}
	gramsMode := request.Grams != nil
	portionMode := request.PortionID != nil || request.Quantity != nil
	if gramsMode == portionMode {
		return Result{}, &ValidationError{Field: "calculation_mode"}
	}

	var rawGrams float64
	if gramsMode {
		if !finitePositive(*request.Grams) {
			return Result{}, &ValidationError{Field: "grams"}
		}
		rawGrams = *request.Grams
	} else {
		if request.PortionID == nil || *request.PortionID <= 0 {
			return Result{}, &ValidationError{Field: "portion_id"}
		}
		if request.Quantity == nil || !finitePositive(*request.Quantity) {
			return Result{}, &ValidationError{Field: "quantity"}
		}
	}

	source, err := s.repository.Load(ctx, request.FoodID, request.PortionID)
	if err != nil {
		if errors.Is(err, ErrFoodNotFound) || errors.Is(err, ErrPortionNotFound) {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("load nutrition source: %w", err)
	}
	if !gramsMode {
		if source.Portion == nil || source.Portion.FoodID != request.FoodID || source.Portion.ID != *request.PortionID {
			return Result{}, ErrPortionNotFound
		}
		rawGrams = source.Portion.Grams * *request.Quantity
	}
	resolvedGrams := roundTwo(rawGrams)
	if !finitePositive(resolvedGrams) {
		return Result{}, &ValidationError{Field: "resolved_grams"}
	}

	calories, err := scale(source.Nutrition.CaloriesPer100g, resolvedGrams)
	if err != nil {
		return Result{}, err
	}
	protein, err := scale(source.Nutrition.ProteinPer100g, resolvedGrams)
	if err != nil {
		return Result{}, err
	}
	carbohydrates, err := scale(source.Nutrition.CarbohydratesPer100g, resolvedGrams)
	if err != nil {
		return Result{}, err
	}
	fat, err := scale(source.Nutrition.FatPer100g, resolvedGrams)
	if err != nil {
		return Result{}, err
	}
	return Result{
		FoodID: request.FoodID, ResolvedGrams: resolvedGrams,
		Nutrition: Nutrition{Calories: calories, Protein: protein, Carbohydrates: carbohydrates, Fat: fat},
	}, nil
}

func scale(per100g food.NutrientAmount, grams float64) (food.NutrientAmount, error) {
	value, known := per100g.Value()
	if !known {
		return food.NutrientAmount{}, nil
	}
	calculated := roundTwo(value * grams / 100)
	if math.IsNaN(calculated) || math.IsInf(calculated, 0) || calculated < 0 {
		return food.NutrientAmount{}, fmt.Errorf("calculated nutrition is outside the supported numeric range")
	}
	amount, err := food.NewNutrientAmount(calculated)
	if err != nil {
		return food.NutrientAmount{}, fmt.Errorf("create calculated nutrient: %w", err)
	}
	return amount, nil
}

func roundTwo(value float64) float64 { return math.Round(value*100) / 100 }

func finitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}
