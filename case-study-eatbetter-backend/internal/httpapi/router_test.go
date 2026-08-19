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

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
)

func TestHealthDoesNotDependOnDatabase(t *testing.T) {
	t.Parallel()

	pingCalled := false
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error {
		pingCalled = true
		return errors.New("database unavailable")
	}, &stubFoodSearcher{})

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

			router := NewRouter(discardLogger(), time.Second, tt.ping, &stubFoodSearcher{})
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
	}, &stubFoodSearcher{})

	response := performRequest(router, http.MethodGet, "/ready")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestHealthEndpointsRejectOtherMethods(t *testing.T) {
	t.Parallel()

	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{})
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
	router := NewRouter(logger, time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{})

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

func TestFoodSearchReturnsSmallStableResponse(t *testing.T) {
	t.Parallel()
	brand := "Example Brand"
	search := &stubFoodSearcher{candidates: []foodsearch.FoodCandidate{{
		FoodID: 42, CanonicalName: "Milk, whole", DisplayName: "Tam yağlı süt", Brand: &brand,
		Match: foodsearch.MatchMetadata{Similarity: 0.99},
	}}}
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, search)

	response := performRequest(router, http.MethodGet, "/foods/search?q=s%C3%BCt&locale=tr-TR&limit=5")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	want := "{\"items\":[{\"food_id\":42,\"display_name\":\"Tam yağlı süt\",\"canonical_name\":\"Milk, whole\",\"brand\":\"Example Brand\"}]}\n"
	if response.Body.String() != want {
		t.Fatalf("body = %q, want %q", response.Body.String(), want)
	}
	if search.request.Query != "süt" || search.request.Locale != "tr-TR" || search.request.Limit != 5 || !search.request.LimitSet {
		t.Fatalf("application request = %+v", search.request)
	}
	assertJSONResponse(t, response)
}

func TestFoodSearchValidation(t *testing.T) {
	t.Parallel()
	service := foodsearch.NewService(&httpSearchRepository{})
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, service)
	tests := []string{
		"/foods/search",
		"/foods/search?q=",
		"/foods/search?q=%20%20",
		"/foods/search?q=a",
		"/foods/search?q=" + strings.Repeat("a", 121),
		"/foods/search?q=milk&limit=wat",
		"/foods/search?q=milk&limit=",
		"/foods/search?q=milk&limit=0",
		"/foods/search?q=milk&limit=-1",
		"/foods/search?q=milk&limit=21",
		"/foods/search?q=milk&locale=tr_TR",
	}
	for _, path := range tests {
		response := performRequest(router, http.MethodGet, path)
		if response.Code != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400", path, response.Code)
		}
		if response.Body.String() != "{\"status\":\"invalid_request\"}\n" {
			t.Errorf("GET %s body = %q", path, response.Body.String())
		}
		assertJSONResponse(t, response)
	}
}

func TestFoodSearchValidUnsupportedLocaleAndEmptyResult(t *testing.T) {
	t.Parallel()
	service := foodsearch.NewService(&httpSearchRepository{})
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, service)
	response := performRequest(router, http.MethodGet, "/foods/search?q=milch&locale=de-DE")
	if response.Code != http.StatusOK || response.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestFoodSearchRejectsOtherMethods(t *testing.T) {
	t.Parallel()
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{})
	response := performRequest(router, http.MethodPost, "/foods/search?q=milk")
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("response = %d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestFoodSearchFailureDoesNotLeakDatabaseDetails(t *testing.T) {
	t.Parallel()
	search := &stubFoodSearcher{err: errors.New("postgres password=secret relation foods")}
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, search)
	response := performRequest(router, http.MethodGet, "/foods/search?q=milk")
	if response.Code != http.StatusInternalServerError || response.Body.String() != "{\"status\":\"internal_error\"}\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "postgres") || strings.Contains(response.Body.String(), "secret") {
		t.Fatal("response leaked database error")
	}
}

type stubFoodSearcher struct {
	request    foodsearch.Request
	candidates []foodsearch.FoodCandidate
	err        error
}

func (s *stubFoodSearcher) Search(_ context.Context, request foodsearch.Request) ([]foodsearch.FoodCandidate, error) {
	s.request = request
	return s.candidates, s.err
}

type httpSearchRepository struct{}

func (*httpSearchRepository) Search(context.Context, foodsearch.Query) ([]foodsearch.FoodCandidate, error) {
	return []foodsearch.FoodCandidate{}, nil
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
