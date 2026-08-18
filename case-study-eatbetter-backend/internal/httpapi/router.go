package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// PingFunc checks whether the database can accept a request.
type PingFunc func(context.Context) error

// NewRouter builds the API's HTTP handler without starting a network listener.
func NewRouter(logger *slog.Logger, readinessTimeout time.Duration, ping PingFunc) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/health", getOnly(http.HandlerFunc(healthHandler)))
	mux.Handle("/ready", getOnly(http.HandlerFunc(readinessHandler(logger, readinessTimeout, ping))))

	return withRequestID(withAccessLog(logger, withRecovery(logger, mux)))
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(struct {
		Status string `json:"status"`
	}{Status: status})
}
