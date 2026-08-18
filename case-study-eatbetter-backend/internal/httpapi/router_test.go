package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthDoesNotDependOnDatabase(t *testing.T) {
	t.Parallel()

	pingCalled := false
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error {
		pingCalled = true
		return errors.New("database unavailable")
	})

	response := performRequest(router, http.MethodGet, "/health")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("body = %q", response.Body.String())
	}
	if pingCalled {
		t.Fatal("health endpoint unexpectedly pinged the database")
	}
	assertJSONResponse(t, response)
}

func TestReadinessReportsDatabaseState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ping       PingFunc
		wantStatus int
		wantBody   string
	}{
		{
			name:       "ready",
			ping:       func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
			wantBody:   "{\"status\":\"ready\"}\n",
		},
		{
			name:       "database unavailable",
			ping:       func(context.Context) error { return errors.New("unavailable") },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "{\"status\":\"not_ready\"}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			router := NewRouter(discardLogger(), time.Second, tt.ping)
			response := performRequest(router, http.MethodGet, "/ready")
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if response.Body.String() != tt.wantBody {
				t.Fatalf("body = %q, want %q", response.Body.String(), tt.wantBody)
			}
			assertJSONResponse(t, response)
		})
	}
}

func TestReadinessAppliesTimeout(t *testing.T) {
	t.Parallel()

	router := NewRouter(discardLogger(), 5*time.Millisecond, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	response := performRequest(router, http.MethodGet, "/ready")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestHealthEndpointsRejectOtherMethods(t *testing.T) {
	t.Parallel()

	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil })
	for _, path := range []string{"/health", "/ready"} {
		response := performRequest(router, http.MethodPost, path)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s status = %d, want %d", path, response.Code, http.StatusMethodNotAllowed)
		}
		if response.Header().Get("Allow") != http.MethodGet {
			t.Fatalf("POST %s Allow = %q, want GET", path, response.Header().Get("Allow"))
		}
		assertJSONResponse(t, response)
	}
}

func TestRequestMiddlewareAddsIDAndLogsRequest(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := NewRouter(logger, time.Second, func(context.Context) error { return nil })

	response := performRequest(router, http.MethodGet, "/health")
	requestID := response.Header().Get(requestIDHeader)
	if requestID == "" {
		t.Fatal("response is missing X-Request-ID")
	}
	for _, field := range []string{"\"request_id\":\"" + requestID + "\"", "\"method\":\"GET\"", "\"path\":\"/health\"", "\"status\":200", "\"duration_ms\":"} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("log %q does not contain %q", logs.String(), field)
		}
	}
}

func TestRecoveryMiddlewareReturnsGenericError(t *testing.T) {
	t.Parallel()

	handler := withRequestID(withAccessLog(discardLogger(), withRecovery(discardLogger(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("sensitive implementation detail")
	}))))

	response := performRequest(handler, http.MethodGet, "/panic")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if response.Body.String() != "{\"status\":\"internal_error\"}\n" {
		t.Fatalf("body = %q", response.Body.String())
	}
	if response.Header().Get(requestIDHeader) == "" {
		t.Fatal("response is missing X-Request-ID")
	}
}

func performRequest(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertJSONResponse(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
