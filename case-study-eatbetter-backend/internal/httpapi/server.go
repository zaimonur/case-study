package httpapi

import (
	"net/http"

	"eatbetter-backend/internal/config"
)

// NewServer configures the API's HTTP server and defensive timeouts.
func NewServer(cfg config.HTTP, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Address(),
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
}
