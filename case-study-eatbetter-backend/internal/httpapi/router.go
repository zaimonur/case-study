package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

const nutritionRequestBodyLimit = 4 * 1024

// PingFunc checks whether the database can accept a request.
type PingFunc func(context.Context) error

// FoodSearcher is the application boundary used by the thin HTTP adapter.
type FoodSearcher interface {
	Search(context.Context, foodsearch.Request) ([]foodsearch.FoodCandidate, error)
}

// FoodDetailer is the application boundary for one canonical food.
type FoodDetailer interface {
	Get(context.Context, fooddetail.Request) (fooddetail.Detail, error)
}

// NutritionCalculator is the deterministic nutrition application boundary.
type NutritionCalculator interface {
	Calculate(context.Context, nutritioncalc.Request) (nutritioncalc.Result, error)
}

// NewRouter builds the API's HTTP handler without starting a network listener.
func NewRouter(
	logger *slog.Logger,
	readinessTimeout time.Duration,
	ping PingFunc,
	search FoodSearcher,
	detail FoodDetailer,
	calculator NutritionCalculator,
	mealAIService MealAIService,
) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/health", getOnly(http.HandlerFunc(healthHandler)))
	mux.Handle("/ready", getOnly(http.HandlerFunc(readinessHandler(logger, readinessTimeout, ping))))
	mux.Handle("/foods/search", getOnly(searchHandler(logger, search)))
	mux.Handle("/foods/", getOnly(foodDetailHandler(logger, detail)))
	mux.Handle("/nutrition/calculate", postOnly(nutritionHandler(logger, calculator)))
	mux.Handle("/ai/meals/interpret", noStore(postOnly(mealInterpretHandler(logger, mealAIService))))
	mux.Handle("/ai/meals/resolve", noStore(postOnly(mealResolveHandler(logger, mealAIService))))

	return withRequestID(withAccessLog(logger, withRecovery(logger, mux)))
}

func foodDetailHandler(logger *slog.Logger, service FoodDetailer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idText := strings.TrimPrefix(r.URL.Path, "/foods/")
		if idText == "" || strings.Contains(idText, "/") {
			writeStatus(w, http.StatusBadRequest, "invalid_request")
			return
		}
		foodID, err := strconv.ParseInt(idText, 10, 64)
		if err != nil || foodID <= 0 {
			writeStatus(w, http.StatusBadRequest, "invalid_request")
			return
		}
		detail, err := service.Get(r.Context(), fooddetail.Request{FoodID: foodID, Locale: r.URL.Query().Get("locale")})
		if err != nil {
			switch {
			case fooddetail.IsValidationError(err):
				writeStatus(w, http.StatusBadRequest, "invalid_request")
			case errors.Is(err, fooddetail.ErrNotFound):
				writeStatus(w, http.StatusNotFound, "food_not_found")
			default:
				logger.ErrorContext(r.Context(), "food detail failed",
					"request_id", requestIDFromContext(r.Context()), "error", err)
				writeStatus(w, http.StatusInternalServerError, "internal_error")
			}
			return
		}
		response := foodDetailResponse{
			FoodID: detail.Food.ID, DisplayName: detail.DisplayName,
			CanonicalName: detail.Food.CanonicalName, Brand: detail.Food.Brand,
			NutritionPer100g: nutritionFromCanonical(detail.Nutrition),
			Portions:         make([]portionResponse, 0, len(detail.Portions)),
		}
		for _, portion := range detail.Portions {
			response.Portions = append(response.Portions, portionResponse{
				PortionID: portion.ID, Amount: portion.Amount, Measure: portion.Measure, Grams: portion.Grams,
			})
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func nutritionHandler(logger *slog.Logger, calculator NutritionCalculator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, nutritionRequestBodyLimit)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var command nutritionRequest
		if err := decoder.Decode(&command); err != nil {
			writeStatus(w, http.StatusBadRequest, "invalid_request")
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			writeStatus(w, http.StatusBadRequest, "invalid_request")
			return
		}
		result, err := calculator.Calculate(r.Context(), nutritioncalc.Request{
			FoodID: command.FoodID, Grams: command.Grams,
			PortionID: command.PortionID, Quantity: command.Quantity,
		})
		if err != nil {
			switch {
			case nutritioncalc.IsValidationError(err):
				writeStatus(w, http.StatusBadRequest, "invalid_request")
			case errors.Is(err, nutritioncalc.ErrFoodNotFound):
				writeStatus(w, http.StatusNotFound, "food_not_found")
			case errors.Is(err, nutritioncalc.ErrPortionNotFound):
				writeStatus(w, http.StatusNotFound, "portion_not_found")
			default:
				logger.ErrorContext(r.Context(), "nutrition calculation failed",
					"request_id", requestIDFromContext(r.Context()), "error", err)
				writeStatus(w, http.StatusInternalServerError, "internal_error")
			}
			return
		}
		writeJSON(w, http.StatusOK, nutritionResponse{
			FoodID: result.FoodID, ResolvedGrams: result.ResolvedGrams,
			Nutrition: nutritionAmounts{
				CaloriesKcal:   nutrientPointer(result.Nutrition.Calories),
				ProteinG:       nutrientPointer(result.Nutrition.Protein),
				CarbohydratesG: nutrientPointer(result.Nutrition.Carbohydrates),
				FatG:           nutrientPointer(result.Nutrition.Fat),
			},
		})
	})
}

type foodDetailResponse struct {
	FoodID           int64             `json:"food_id"`
	DisplayName      string            `json:"display_name"`
	CanonicalName    string            `json:"canonical_name"`
	Brand            *string           `json:"brand"`
	NutritionPer100g nutritionAmounts  `json:"nutrition_per_100g"`
	Portions         []portionResponse `json:"portions"`
}

type portionResponse struct {
	PortionID int64   `json:"portion_id"`
	Amount    float64 `json:"amount"`
	Measure   string  `json:"measure"`
	Grams     float64 `json:"grams"`
}

type nutritionRequest struct {
	FoodID    int64    `json:"food_id"`
	Grams     *float64 `json:"grams"`
	PortionID *int64   `json:"portion_id"`
	Quantity  *float64 `json:"quantity"`
}

type nutritionResponse struct {
	FoodID        int64            `json:"food_id"`
	ResolvedGrams float64          `json:"resolved_grams"`
	Nutrition     nutritionAmounts `json:"nutrition"`
}

type nutritionAmounts struct {
	CaloriesKcal   *float64 `json:"calories_kcal"`
	ProteinG       *float64 `json:"protein_g"`
	CarbohydratesG *float64 `json:"carbohydrates_g"`
	FatG           *float64 `json:"fat_g"`
}

func nutritionFromCanonical(nutrition *food.Nutrition) nutritionAmounts {
	if nutrition == nil {
		return nutritionAmounts{}
	}
	return nutritionAmounts{
		CaloriesKcal:   nutrientPointer(nutrition.CaloriesPer100g),
		ProteinG:       nutrientPointer(nutrition.ProteinPer100g),
		CarbohydratesG: nutrientPointer(nutrition.CarbohydratesPer100g),
		FatG:           nutrientPointer(nutrition.FatPer100g),
	}
}

func nutrientPointer(amount food.NutrientAmount) *float64 {
	value, known := amount.Value()
	if !known {
		return nil
	}
	return &value
}

func searchHandler(logger *slog.Logger, search FoodSearcher) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := 0
		if r.URL.Query().Has("limit") {
			parsed, err := strconv.Atoi(r.URL.Query().Get("limit"))
			if err != nil {
				writeStatus(w, http.StatusBadRequest, "invalid_request")
				return
			}
			limit = parsed
		}

		candidates, err := search.Search(r.Context(), foodsearch.Request{
			Query: r.URL.Query().Get("q"), Locale: r.URL.Query().Get("locale"),
			Limit: limit, LimitSet: r.URL.Query().Has("limit"),
		})
		if err != nil {
			if foodsearch.IsValidationError(err) {
				writeStatus(w, http.StatusBadRequest, "invalid_request")
				return
			}
			logger.ErrorContext(r.Context(), "food search failed",
				"request_id", requestIDFromContext(r.Context()), "error", err)
			writeStatus(w, http.StatusInternalServerError, "internal_error")
			return
		}

		items := make([]foodSearchItem, 0, len(candidates))
		for _, candidate := range candidates {
			items = append(items, foodSearchItem{
				FoodID: candidate.FoodID, DisplayName: candidate.DisplayName,
				CanonicalName: candidate.CanonicalName, Brand: candidate.Brand,
			})
		}
		writeJSON(w, http.StatusOK, struct {
			Items []foodSearchItem `json:"items"`
		}{Items: items})
	})
}

type foodSearchItem struct {
	FoodID        int64   `json:"food_id"`
	DisplayName   string  `json:"display_name"`
	CanonicalName string  `json:"canonical_name"`
	Brand         *string `json:"brand"`
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeStatus(w, http.StatusOK, "ok")
}

func readinessHandler(logger *slog.Logger, timeout time.Duration, ping PingFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		if err := ping(ctx); err != nil {
			logger.WarnContext(ctx, "readiness check failed", "error", err)
			writeStatus(w, http.StatusServiceUnavailable, "not_ready")
			return
		}

		writeStatus(w, http.StatusOK, "ready")
	}
}

func getOnly(next http.Handler) http.Handler {
	return methodOnly(http.MethodGet, next)
}

func postOnly(next http.Handler) http.Handler {
	return methodOnly(http.MethodPost, next)
}

func methodOnly(method string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeStatus(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeStatus(w http.ResponseWriter, statusCode int, status string) {
	writeJSON(w, statusCode, struct {
		Status string `json:"status"`
	}{Status: status})
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
