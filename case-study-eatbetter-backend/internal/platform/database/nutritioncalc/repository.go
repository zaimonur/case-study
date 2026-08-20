// Package nutritioncalc loads canonical nutrition and owned trusted portions from PostgreSQL.
package nutritioncalc

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Repository is the focused calculation source adapter.
type Repository struct{ database queryer }

func New(database queryer) *Repository { return &Repository{database: database} }

// Load selects a portion only when both its ID and owning food ID match.
func (r *Repository) Load(ctx context.Context, foodID int64, portionID *int64) (app.Source, error) {
	var (
		nutritionFoodID                       int64
		calories, protein, carbohydrates, fat *float64
		storedID, storedFoodID                *int64
		amount, grams                         *float64
		measure                               *string
	)
	err := r.database.QueryRow(ctx, sourceSQL, foodID, portionID).Scan(
		&nutritionFoodID, &calories, &protein, &carbohydrates, &fat,
		&storedID, &storedFoodID, &amount, &measure, &grams,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Source{}, app.ErrFoodNotFound
	}
	if err != nil {
		return app.Source{}, fmt.Errorf("query nutrition source: %w", err)
	}
	if nutritionFoodID == 0 {
		return app.Source{}, fmt.Errorf("canonical food has no nutrition row")
	}
	nutrition, err := mapNutrition(nutritionFoodID, calories, protein, carbohydrates, fat)
	if err != nil {
		return app.Source{}, fmt.Errorf("map canonical nutrition: %w", err)
	}
	source := app.Source{Nutrition: nutrition}
	if portionID != nil {
		if storedID == nil || storedFoodID == nil || amount == nil || measure == nil || grams == nil {
			return app.Source{}, app.ErrPortionNotFound
		}
		portion, err := food.NewPortion(*storedFoodID, *amount, *measure, *grams)
		if err != nil {
			return app.Source{}, fmt.Errorf("map stored portion: %w", err)
		}
		portion.ID = *storedID
		source.Portion = &portion
	}
	return source, nil
}

func mapNutrition(foodID int64, values ...*float64) (food.Nutrition, error) {
	amounts := make([]food.NutrientAmount, len(values))
	for index, value := range values {
		if value == nil {
			continue
		}
		amount, err := food.NewNutrientAmount(*value)
		if err != nil {
			return food.Nutrition{}, err
		}
		amounts[index] = amount
	}
	return food.NewNutrition(foodID, amounts[0], amounts[1], amounts[2], amounts[3])
}

const sourceSQL = `
SELECT COALESCE(nutrition.food_id, 0),
       nutrition.calories_per_100g,
       nutrition.protein_per_100g,
       nutrition.carbohydrates_per_100g,
       nutrition.fat_per_100g,
       portion.id,
       portion.food_id,
       portion.amount,
       portion.measure,
       portion.grams
FROM foods food
LEFT JOIN food_nutrition nutrition ON nutrition.food_id = food.id
LEFT JOIN food_portions portion ON portion.food_id = food.id AND portion.id = $2
WHERE food.id = $1`
