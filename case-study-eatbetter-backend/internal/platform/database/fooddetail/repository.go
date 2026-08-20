// Package fooddetail implements PostgreSQL-backed canonical food detail retrieval.
package fooddetail

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Repository is the focused food-detail read adapter.
type Repository struct{ database queryer }

func New(database queryer) *Repository { return &Repository{database: database} }

// Get uses a fixed two-query plan: food/nutrition/display, then ordered portions.
func (r *Repository) Get(ctx context.Context, query app.Query) (app.Detail, error) {
	var (
		id, nutritionFoodID                   int64
		canonicalName, displayName            string
		brand                                 *string
		calories, protein, carbohydrates, fat *float64
	)
	err := r.database.QueryRow(ctx, detailSQL, query.FoodID, query.Locale, query.BaseLocale).Scan(
		&id, &canonicalName, &brand, &displayName, &nutritionFoodID,
		&calories, &protein, &carbohydrates, &fat,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Detail{}, app.ErrNotFound
	}
	if err != nil {
		return app.Detail{}, fmt.Errorf("query food detail: %w", err)
	}

	canonicalFood, err := food.NewFood(canonicalName, brand)
	if err != nil {
		return app.Detail{}, fmt.Errorf("map food detail: %w", err)
	}
	canonicalFood.ID = id
	nutrition, err := mapNutrition(nutritionFoodID, calories, protein, carbohydrates, fat)
	if err != nil {
		return app.Detail{}, fmt.Errorf("map food nutrition: %w", err)
	}

	rows, err := r.database.Query(ctx, portionsSQL, query.FoodID)
	if err != nil {
		return app.Detail{}, fmt.Errorf("query food portions: %w", err)
	}
	defer rows.Close()
	portions := make([]food.Portion, 0)
	for rows.Next() {
		var id int64
		var amount, grams float64
		var measure string
		if err := rows.Scan(&id, &amount, &measure, &grams); err != nil {
			return app.Detail{}, fmt.Errorf("scan food portion: %w", err)
		}
		portion, err := food.NewPortion(query.FoodID, amount, measure, grams)
		if err != nil {
			return app.Detail{}, fmt.Errorf("map food portion: %w", err)
		}
		portion.ID = id
		portions = append(portions, portion)
	}
	if err := rows.Err(); err != nil {
		return app.Detail{}, fmt.Errorf("iterate food portions: %w", err)
	}
	return app.Detail{Food: canonicalFood, DisplayName: displayName, Nutrition: nutrition, Portions: portions}, nil
}

func mapNutrition(foodID int64, values ...*float64) (*food.Nutrition, error) {
	if foodID == 0 {
		return nil, nil
	}
	amounts := make([]food.NutrientAmount, len(values))
	for index, value := range values {
		if value == nil {
			continue
		}
		amount, err := food.NewNutrientAmount(*value)
		if err != nil {
			return nil, err
		}
		amounts[index] = amount
	}
	nutrition, err := food.NewNutrition(foodID, amounts[0], amounts[1], amounts[2], amounts[3])
	if err != nil {
		return nil, err
	}
	return &nutrition, nil
}

const detailSQL = `
SELECT food.id,
       food.canonical_name,
       food.brand,
       COALESCE(display.display_name, food.canonical_name),
       COALESCE(nutrition.food_id, 0),
       nutrition.calories_per_100g,
       nutrition.protein_per_100g,
       nutrition.carbohydrates_per_100g,
       nutrition.fat_per_100g
FROM foods food
LEFT JOIN food_nutrition nutrition ON nutrition.food_id = food.id
LEFT JOIN LATERAL (
    SELECT localization.display_name
    FROM food_localizations localization
    WHERE localization.food_id = food.id
      AND localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($2, $3)
    ORDER BY CASE WHEN localization.locale = $2 THEN 0 ELSE 1 END, localization.id
    LIMIT 1
) display ON TRUE
WHERE food.id = $1`

const portionsSQL = `
SELECT id, amount, measure, grams
FROM food_portions
WHERE food_id = $1
ORDER BY amount, measure, grams, id`
