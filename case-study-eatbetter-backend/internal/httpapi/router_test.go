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

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

func TestHealthDoesNotDependOnDatabase(t *testing.T) {
	t.Parallel()

	pingCalled := false
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error {
		pingCalled = true
		return errors.New("database unavailable")
	}, &stubFoodSearcher{}, nil, nil, nil)

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

			router := NewRouter(discardLogger(), time.Second, tt.ping, &stubFoodSearcher{}, nil, nil, nil)
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
	}, &stubFoodSearcher{}, nil, nil, nil)

	response := performRequest(router, http.MethodGet, "/ready")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestHealthEndpointsRejectOtherMethods(t *testing.T) {
	t.Parallel()

	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, nil, nil)
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
	router := NewRouter(logger, time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, nil, nil)

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
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, search, nil, nil, nil)

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
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, service, nil, nil, nil)
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
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, service, nil, nil, nil)
	response := performRequest(router, http.MethodGet, "/foods/search?q=milch&locale=de-DE")
	if response.Code != http.StatusOK || response.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestFoodSearchRejectsOtherMethods(t *testing.T) {
	t.Parallel()
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, nil, nil)
	response := performRequest(router, http.MethodPost, "/foods/search?q=milk")
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("response = %d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestFoodSearchFailureDoesNotLeakDatabaseDetails(t *testing.T) {
	t.Parallel()
	search := &stubFoodSearcher{err: errors.New("postgres password=secret relation foods")}
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, search, nil, nil, nil)
	response := performRequest(router, http.MethodGet, "/foods/search?q=milk")
	if response.Code != http.StatusInternalServerError || response.Body.String() != "{\"status\":\"internal_error\"}\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "postgres") || strings.Contains(response.Body.String(), "secret") {
		t.Fatal("response leaked database error")
	}
}

func TestFoodDetailSuccess(t *testing.T) {
	t.Parallel()
	brand := "Example"
	zero, _ := food.NewNutrientAmount(0)
	protein, _ := food.NewNutrientAmount(4.5)
	nutrition, _ := food.NewNutrition(42, zero, protein, food.NutrientAmount{}, food.NutrientAmount{})
	portion, _ := food.NewPortion(42, 1, "slice", 28)
	portion.ID = 9
	service := &stubFoodDetailer{detail: fooddetail.Detail{
		Food: food.Food{ID: 42, CanonicalName: "Milk", Brand: &brand}, DisplayName: "Süt",
		Nutrition: &nutrition, Portions: []food.Portion{portion},
	}}
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, service, nil, nil)
	response := performRequest(router, http.MethodGet, "/foods/42?locale=tr-TR")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	want := "{\"food_id\":42,\"display_name\":\"Süt\",\"canonical_name\":\"Milk\",\"brand\":\"Example\",\"nutrition_per_100g\":{\"calories_kcal\":0,\"protein_g\":4.5,\"carbohydrates_g\":null,\"fat_g\":null},\"portions\":[{\"portion_id\":9,\"amount\":1,\"measure\":\"slice\",\"grams\":28}]}\n"
	if response.Body.String() != want {
		t.Fatalf("body = %q, want %q", response.Body.String(), want)
	}
	if service.request != (fooddetail.Request{FoodID: 42, Locale: "tr-TR"}) {
		t.Fatalf("request = %+v", service.request)
	}
	assertJSONResponse(t, response)
	if response.Header().Get(requestIDHeader) == "" {
		t.Fatal("missing X-Request-ID")
	}
}

func TestFoodDetailErrorsAndMethod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		err  error
		want int
		body string
	}{
		{"/foods/not-an-id", nil, 400, "invalid_request"},
		{"/foods/0", nil, 400, "invalid_request"},
		{"/foods/-1", nil, 400, "invalid_request"},
		{"/foods/1/extra", nil, 400, "invalid_request"},
		{"/foods/1?locale=tr_TR", &fooddetail.ValidationError{Field: "locale"}, 400, "invalid_request"},
		{"/foods/999", fooddetail.ErrNotFound, 404, "food_not_found"},
	}
	for _, test := range tests {
		service := &stubFoodDetailer{err: test.err}
		router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, service, nil, nil)
		response := performRequest(router, http.MethodGet, test.path)
		if response.Code != test.want || response.Body.String() != "{\"status\":\""+test.body+"\"}\n" {
			t.Errorf("GET %s = %d %q", test.path, response.Code, response.Body.String())
		}
		assertJSONResponse(t, response)
	}
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, &stubFoodDetailer{}, nil, nil)
	response := performRequest(router, http.MethodPost, "/foods/1")
	if response.Code != 405 || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST detail = %d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestNutritionDirectAndPortionRequests(t *testing.T) {
	t.Parallel()
	knownZero, _ := food.NewNutrientAmount(0)
	protein, _ := food.NewNutrientAmount(5.34)
	calculator := &stubNutritionCalculator{result: nutritioncalc.Result{
		FoodID: 1, ResolvedGrams: 56,
		Nutrition: nutritioncalc.Nutrition{Calories: knownZero, Protein: protein},
	}}
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, calculator, nil)
	response := performRequestWithBody(router, http.MethodPost, "/nutrition/calculate", `{"food_id":1,"grams":56}`)
	if response.Code != 200 || response.Body.String() != "{\"food_id\":1,\"resolved_grams\":56,\"nutrition\":{\"calories_kcal\":0,\"protein_g\":5.34,\"carbohydrates_g\":null,\"fat_g\":null}}\n" {
		t.Fatalf("direct response = %d %q", response.Code, response.Body.String())
	}
	if calculator.request.Grams == nil || *calculator.request.Grams != 56 {
		t.Fatalf("direct request = %+v", calculator.request)
	}
	response = performRequestWithBody(router, http.MethodPost, "/nutrition/calculate", `{"food_id":1,"portion_id":9,"quantity":2}`)
	if response.Code != 200 || calculator.request.PortionID == nil || calculator.request.Quantity == nil {
		t.Fatalf("portion response/request = %d %+v", response.Code, calculator.request)
	}
}

func TestNutritionRejectsMalformedBoundedJSON(t *testing.T) {
	t.Parallel()
	calculator := &stubNutritionCalculator{}
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, calculator, nil)
	for _, body := range []string{
		`{`,
		`{"food_id":1,"grams":10,"unknown":true}`,
		`{"food_id":1,"grams":10} {"food_id":1,"grams":10}`,
		`{"food_id":1,"grams":10,"padding":"` + strings.Repeat("x", nutritionRequestBodyLimit) + `"}`,
	} {
		response := performRequestWithBody(router, http.MethodPost, "/nutrition/calculate", body)
		if response.Code != 400 || response.Body.String() != "{\"status\":\"invalid_request\"}\n" {
			t.Errorf("body length %d response = %d %q", len(body), response.Code, response.Body.String())
		}
	}
}

func TestNutritionPresenceValidationAndApplicationErrors(t *testing.T) {
	t.Parallel()
	repository := &httpNutritionRepository{source: canonicalNutritionSource(t)}
	service := nutritioncalc.NewService(repository)
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, service, nil)
	for _, body := range []string{
		`{"food_id":1,"grams":0}`,
		`{"food_id":1}`,
		`{"food_id":1,"grams":10,"portion_id":2,"quantity":1}`,
		`{"food_id":1,"portion_id":2,"quantity":0}`,
	} {
		response := performRequestWithBody(router, http.MethodPost, "/nutrition/calculate", body)
		if response.Code != 400 {
			t.Errorf("%s status = %d, want 400", body, response.Code)
		}
	}
	for err, wantStatus := range map[error]string{
		nutritioncalc.ErrFoodNotFound:    "food_not_found",
		nutritioncalc.ErrPortionNotFound: "portion_not_found",
	} {
		calculator := &stubNutritionCalculator{err: err}
		router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, calculator, nil)
		response := performRequestWithBody(router, http.MethodPost, "/nutrition/calculate", `{"food_id":1,"grams":10}`)
		if response.Code != 404 || response.Body.String() != "{\"status\":\""+wantStatus+"\"}\n" {
			t.Errorf("error %v response = %d %q", err, response.Code, response.Body.String())
		}
	}
}

func TestNutritionMethodAndSanitizedInternalError(t *testing.T) {
	t.Parallel()
	calculator := &stubNutritionCalculator{err: errors.New("postgres relation food_nutrition password=secret")}
	router := NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, calculator, nil)
	response := performRequestWithBody(router, http.MethodPost, "/nutrition/calculate", `{"food_id":1,"grams":10}`)
	if response.Code != 500 || response.Body.String() != "{\"status\":\"internal_error\"}\n" || strings.Contains(response.Body.String(), "postgres") {
		t.Fatalf("internal response = %d %q", response.Code, response.Body.String())
	}
	response = performRequest(router, http.MethodGet, "/nutrition/calculate")
	if response.Code != 405 || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET nutrition = %d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

type stubFoodSearcher struct {
	request    foodsearch.Request
	candidates []foodsearch.FoodCandidate
	err        error
}

type stubFoodDetailer struct {
	request fooddetail.Request
	detail  fooddetail.Detail
	err     error
}

func (s *stubFoodDetailer) Get(_ context.Context, request fooddetail.Request) (fooddetail.Detail, error) {
	s.request = request
	return s.detail, s.err
}

type stubNutritionCalculator struct {
	request nutritioncalc.Request
	result  nutritioncalc.Result
	err     error
}

func (s *stubNutritionCalculator) Calculate(_ context.Context, request nutritioncalc.Request) (nutritioncalc.Result, error) {
	s.request = request
	return s.result, s.err
}

type httpNutritionRepository struct {
	source nutritioncalc.Source
	err    error
}

func (r *httpNutritionRepository) Load(context.Context, int64, *int64) (nutritioncalc.Source, error) {
	return r.source, r.err
}

func canonicalNutritionSource(t *testing.T) nutritioncalc.Source {
	t.Helper()
	calories, _ := food.NewNutrientAmount(100)
	nutrition, err := food.NewNutrition(1, calories, food.NutrientAmount{}, food.NutrientAmount{}, food.NutrientAmount{})
	if err != nil {
		t.Fatal(err)
	}
	return nutritioncalc.Source{Nutrition: nutrition}
}

func (s *stubFoodSearcher) Search(_ context.Context, request foodsearch.Request) ([]foodsearch.FoodCandidate, error) {
	s.request = request
	return s.candidates, s.err
}

type httpSearchRepository struct{}

func (*httpSearchRepository) Search(context.Context, foodsearch.Query) ([]foodsearch.FoodCandidate, error) {
	return []foodsearch.FoodCandidate{}, nil
}

func (*httpSearchRepository) ResolveBrand(context.Context, []foodsearch.BrandPhrase) (*foodsearch.BrandMatch, error) {
	return nil, nil
}

func (*httpSearchRepository) SearchBranded(context.Context, foodsearch.BrandedQuery) ([]foodsearch.FoodCandidate, error) {
	return []foodsearch.FoodCandidate{}, nil
}

func performRequest(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	return performRequestWithBody(handler, method, path, "")
}

func performRequestWithBody(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
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
