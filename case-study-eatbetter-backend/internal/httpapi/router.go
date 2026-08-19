package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
)

// PingFunc checks whether the database can accept a request.
type PingFunc func(context.Context) error

// FoodSearcher is the application boundary used by the thin HTTP adapter.
type FoodSearcher interface {
	Search(context.Context, foodsearch.Request) ([]foodsearch.FoodCandidate, error)
}

// NewRouter builds the API's HTTP handler without starting a network listener.
func NewRouter(logger *slog.Logger, readinessTimeout time.Duration, ping PingFunc, search FoodSearcher) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/health", getOnly(http.HandlerFunc(healthHandler)))
	mux.Handle("/ready", getOnly(http.HandlerFunc(readinessHandler(logger, readinessTimeout, ping))))
	mux.Handle("/foods/search", getOnly(searchHandler(logger, search)))

	return withRequestID(withAccessLog(logger, withRecovery(logger, mux)))
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
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
