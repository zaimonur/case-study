package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	dbfooddetail "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/fooddetail"
	dbfoodsearch "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/foodsearch"
	dbnutritioncalc "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/nutritioncalc"
)

func TestPhase6CoreAcceptanceFlow(t *testing.T) {
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

	var foodID, portionID int64
	if err := pool.QueryRow(ctx, `INSERT INTO foods (canonical_name) VALUES ('Banana, raw') RETURNING id`).Scan(&foodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO food_nutrition (food_id, calories_per_100g, protein_per_100g, carbohydrates_per_100g, fat_per_100g) VALUES ($1, 89, 1.09, 22.84, 0.33)`, foodID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO food_portions (food_id, amount, measure, grams) VALUES ($1, 1, 'medium', 118) RETURNING id`, foodID).Scan(&portionID); err != nil {
		t.Fatal(err)
	}

	searchService := foodsearch.NewService(dbfoodsearch.New(pool))
	detailService := fooddetail.NewService(dbfooddetail.New(pool))
	calculationService := nutritioncalc.NewService(dbnutritioncalc.New(pool))
	router := NewRouter(discardLogger(), time.Second, pool.Ping, searchService, detailService, calculationService, nil)

	searchResponse := performRequest(router, http.MethodGet, "/foods/search?q=banana&locale=en&limit=5")
	if searchResponse.Code != 200 {
		t.Fatalf("search = %d %s", searchResponse.Code, searchResponse.Body.String())
	}
	var searchPayload struct {
		Items []struct {
			FoodID int64 `json:"food_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(searchResponse.Body.Bytes(), &searchPayload); err != nil || len(searchPayload.Items) == 0 || searchPayload.Items[0].FoodID != foodID {
		t.Fatalf("search payload = %+v, error = %v", searchPayload, err)
	}

	detailResponse := performRequest(router, http.MethodGet, "/foods/"+formatID(foodID)+"?locale=en")
	if detailResponse.Code != 200 {
		t.Fatalf("detail = %d %s", detailResponse.Code, detailResponse.Body.String())
	}
	var detailPayload struct {
		Portions []struct {
			PortionID int64 `json:"portion_id"`
		} `json:"portions"`
	}
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detailPayload); err != nil || len(detailPayload.Portions) != 1 || detailPayload.Portions[0].PortionID != portionID {
		t.Fatalf("detail payload = %+v, error = %v", detailPayload, err)
	}

	body := `{"food_id":` + formatID(foodID) + `,"portion_id":` + formatID(portionID) + `,"quantity":2}`
	first := performRequestWithBody(router, http.MethodPost, "/nutrition/calculate", body)
	second := performRequestWithBody(router, http.MethodPost, "/nutrition/calculate", body)
	if first.Code != 200 || second.Code != 200 || first.Body.String() != second.Body.String() {
		t.Fatalf("calculation responses = %d %q and %d %q", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	var calculation struct {
		ResolvedGrams float64 `json:"resolved_grams"`
		Nutrition     struct {
			Calories *float64 `json:"calories_kcal"`
		} `json:"nutrition"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &calculation); err != nil || calculation.ResolvedGrams != 236 || calculation.Nutrition.Calories == nil || *calculation.Nutrition.Calories != 210.04 {
		t.Fatalf("calculation = %+v, error = %v", calculation, err)
	}
}

func formatID(value int64) string {
	return strconv.FormatInt(value, 10)
}
