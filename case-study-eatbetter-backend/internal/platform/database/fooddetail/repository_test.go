package fooddetail

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
)

func TestRepositoryIntegration(t *testing.T) {
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

	var foodID int64
	if err := pool.QueryRow(ctx, `INSERT INTO foods (canonical_name) VALUES ('Milk, whole') RETURNING id`).Scan(&foodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO food_nutrition (food_id, calories_per_100g, protein_per_100g) VALUES ($1, 0, NULL)`, foodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO food_localizations (food_id, locale, display_name, source_canonical_name, source_fingerprint)
        VALUES ($1, 'tr', 'Tam yağlı süt', 'Milk, whole', 'sha256:' || repeat('0', 64))`, foodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO food_portions (food_id, amount, measure, grams) VALUES
        ($1, 1, 'slice', 28), ($1, 0.5, 'cup', 120), ($1, 1, 'cup', 240)`, foodID); err != nil {
		t.Fatal(err)
	}

	service := app.NewService(New(pool))
	detail, err := service.Get(ctx, app.Request{FoodID: foodID, Locale: "tr-TR"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.DisplayName != "Tam yağlı süt" || detail.Food.Brand != nil || len(detail.Portions) != 3 {
		t.Fatalf("detail = %+v", detail)
	}
	if value, known := detail.Nutrition.CaloriesPer100g.Value(); !known || value != 0 {
		t.Fatalf("known zero calories = (%v, %v)", value, known)
	}
	if detail.Nutrition.ProteinPer100g.IsKnown() {
		t.Fatal("missing protein became known")
	}
	if detail.Portions[0].Amount != 0.5 || detail.Portions[1].Measure != "cup" || detail.Portions[2].Measure != "slice" {
		t.Fatalf("portion ordering = %+v", detail.Portions)
	}

	unsupported, err := service.Get(ctx, app.Request{FoodID: foodID, Locale: "de-DE"})
	if err != nil || unsupported.DisplayName != "Milk, whole" {
		t.Fatalf("unsupported locale detail = %+v, %v", unsupported, err)
	}

	var staleID int64
	brand := "Example Brand"
	if err := pool.QueryRow(ctx, `INSERT INTO foods (canonical_name, brand) VALUES ('Updated food', $1) RETURNING id`, brand).Scan(&staleID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO food_nutrition (food_id, fat_per_100g) VALUES ($1, 1)`, staleID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO food_localizations (food_id, locale, display_name, source_canonical_name, source_fingerprint)
        VALUES ($1, 'tr', 'Bayat ad', 'Old food', 'sha256:' || repeat('0', 64))`, staleID); err != nil {
		t.Fatal(err)
	}
	stale, err := service.Get(ctx, app.Request{FoodID: staleID, Locale: "tr"})
	if err != nil || stale.DisplayName != "Updated food" || stale.Food.Brand == nil || *stale.Food.Brand != brand || len(stale.Portions) != 0 {
		t.Fatalf("stale/branded detail = %+v, %v", stale, err)
	}
	if _, err := service.Get(ctx, app.Request{FoodID: 999999}); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("not-found error = %v", err)
	}
}
