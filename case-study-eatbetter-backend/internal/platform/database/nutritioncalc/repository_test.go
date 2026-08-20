package nutritioncalc

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
)

func TestRepositoryIntegrationEnforcesPortionOwnership(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE foods RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `TRUNCATE foods RESTART IDENTITY CASCADE`) })

	ids := make([]int64, 2)
	for index := range ids {
		if err := pool.QueryRow(ctx, `INSERT INTO foods (canonical_name) VALUES ($1) RETURNING id`, "Food").Scan(&ids[index]); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO food_nutrition (food_id, calories_per_100g, protein_per_100g) VALUES ($1, 100, 0)`, ids[index]); err != nil {
			t.Fatal(err)
		}
	}
	var portionID int64
	if err := pool.QueryRow(ctx, `INSERT INTO food_portions (food_id, amount, measure, grams) VALUES ($1, 0.5, 'cup free text', 120) RETURNING id`, ids[0]).Scan(&portionID); err != nil {
		t.Fatal(err)
	}

	service := app.NewService(New(pool))
	quantity := 2.0
	result, err := service.Calculate(ctx, app.Request{FoodID: ids[0], PortionID: &portionID, Quantity: &quantity})
	if err != nil || result.ResolvedGrams != 240 {
		t.Fatalf("portion result = %+v, %v", result, err)
	}
	if _, err := service.Calculate(ctx, app.Request{FoodID: ids[1], PortionID: &portionID, Quantity: &quantity}); !errors.Is(err, app.ErrPortionNotFound) {
		t.Fatalf("ownership error = %v", err)
	}
	grams := 50.0
	result, err = service.Calculate(ctx, app.Request{FoodID: ids[0], Grams: &grams})
	if err != nil || result.ResolvedGrams != 50 {
		t.Fatalf("direct result = %+v, %v", result, err)
	}
	if _, err := service.Calculate(ctx, app.Request{FoodID: 99999, Grams: &grams}); !errors.Is(err, app.ErrFoodNotFound) {
		t.Fatalf("food error = %v", err)
	}
}
