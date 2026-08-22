package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealai"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type stubMealTextInterpreter struct {
	request mealai.Request
	result  mealai.Result
	err     error
	calls   int
	ctx     context.Context
}

func (stub *stubMealTextInterpreter) InterpretText(ctx context.Context, request mealai.Request) (mealai.Result, error) {
	stub.calls++
	stub.ctx = ctx
	stub.request = request
	return stub.result, stub.err
}

func TestMealInterpretIsPostOnly(t *testing.T) {
	t.Parallel()

	router := mealRouter(&stubMealTextInterpreter{})
	response := performRequest(router, http.MethodGet, "/ai/meals/interpret")
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("response = %d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestMealInterpretForwardsValidRequestAndAppliesMiddleware(t *testing.T) {
	t.Parallel()

	interpreter := &stubMealTextInterpreter{result: mealai.Result{State: mealai.StateEmpty, Items: []mealai.Item{}}}
	router := mealRouter(interpreter)
	response := performRequestWithBody(router, http.MethodPost, "/ai/meals/interpret", `{"text":"2 yumurta","locale":"TR-tr"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if interpreter.calls != 1 || interpreter.request != (mealai.Request{Text: "2 yumurta", Locale: "TR-tr"}) {
		t.Fatalf("calls/request = %d/%#v", interpreter.calls, interpreter.request)
	}
	if requestIDFromContext(interpreter.ctx) == "" || response.Header().Get(requestIDHeader) == "" {
		t.Fatal("request ID middleware did not apply")
	}
	assertJSONResponse(t, response)
}

func TestMealInterpretRejectsMalformedBoundedJSON(t *testing.T) {
	t.Parallel()

	interpreter := &stubMealTextInterpreter{}
	router := mealRouter(interpreter)
	tests := []string{
		`{`,
		`{"text":"elma","locale":"tr","extra":true}`,
		`{"text":"elma","locale":"tr"} {"text":"muz","locale":"tr"}`,
		`{"text":"` + strings.Repeat("x", mealInterpretRequestBodyLimit) + `","locale":"tr"}`,
	}
	for _, body := range tests {
		response := performRequestWithBody(router, http.MethodPost, "/ai/meals/interpret", body)
		if response.Code != http.StatusBadRequest || response.Body.String() != "{\"status\":\"invalid_request\"}\n" {
			t.Errorf("body length %d response = %d %q", len(body), response.Code, response.Body.String())
		}
	}
	if interpreter.calls != 0 {
		t.Fatalf("interpreter calls = %d, want 0", interpreter.calls)
	}
}

func TestMealInterpretMapsApplicationErrorsWithoutLeaks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "invalid input", err: &mealai.Error{Kind: mealai.ErrorInvalidInput}, wantStatus: 400, wantBody: "invalid_request"},
		{name: "AI unavailable", err: &mealai.Error{Kind: mealai.ErrorAIUnavailable}, wantStatus: 503, wantBody: "ai_unavailable"},
		{name: "AI rate limited", err: &mealai.Error{Kind: mealai.ErrorAIRateLimited}, wantStatus: 429, wantBody: "ai_rate_limited"},
		{name: "AI timeout", err: &mealai.Error{Kind: mealai.ErrorAITimeout}, wantStatus: 504, wantBody: "ai_timeout"},
		{name: "invalid AI response", err: &mealai.Error{Kind: mealai.ErrorAIInvalidResponse}, wantStatus: 502, wantBody: "ai_invalid_response"},
		{name: "AI failure", err: &mealai.Error{Kind: mealai.ErrorAIFailure}, wantStatus: 502, wantBody: "ai_provider_error"},
		{name: "dependency timeout", err: &mealai.Error{Kind: mealai.ErrorTimeout}, wantStatus: 504, wantBody: "dependency_timeout"},
		{name: "canceled", err: &mealai.Error{Kind: mealai.ErrorCanceled}, wantStatus: 408, wantBody: "request_canceled"},
		{name: "resolution failure", err: &mealai.Error{Kind: mealai.ErrorResolutionFailure}, wantStatus: 500, wantBody: "internal_error"},
		{name: "unknown failure", err: errors.New("postgres password=sensitive-provider-detail"), wantStatus: 500, wantBody: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			interpreter := &stubMealTextInterpreter{err: tt.err}
			router := NewRouter(logger, time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, nil, interpreter)
			response := performRequestWithBody(router, http.MethodPost, "/ai/meals/interpret", `{"text":"private meal text","locale":"tr"}`)
			if response.Code != tt.wantStatus || response.Body.String() != "{\"status\":\""+tt.wantBody+"\"}\n" {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
			for _, sensitive := range []string{"private meal text", "postgres", "password", "sensitive-provider-detail"} {
				if strings.Contains(response.Body.String(), sensitive) || strings.Contains(logs.String(), sensitive) {
					t.Fatalf("response/log exposed %q", sensitive)
				}
			}
		})
	}
}

func TestMealInterpretWithoutConfiguredExtractorReturnsAIUnavailable(t *testing.T) {
	t.Parallel()

	service := mealai.NewService(foodextraction.NewService(nil), nil, nil)
	response := performRequestWithBody(mealRouter(service), http.MethodPost, "/ai/meals/interpret", `{"text":"elma","locale":"tr"}`)
	if response.Code != http.StatusServiceUnavailable || response.Body.String() != "{\"status\":\"ai_unavailable\"}\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestMealInterpretSerializesEmptyResultWithEmptyItems(t *testing.T) {
	t.Parallel()

	response := performRequestWithBody(
		mealRouter(&stubMealTextInterpreter{result: mealai.Result{State: mealai.StateEmpty, Items: []mealai.Item{}}}),
		http.MethodPost, "/ai/meals/interpret", `{"text":"hiçbir şey yemedim","locale":"tr"}`,
	)
	if response.Code != 200 || response.Body.String() != "{\"state\":\"empty\",\"items\":[]}\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestMealInterpretSerializesReadySelectionsAndNullSemantics(t *testing.T) {
	t.Parallel()

	quantity, unit := 200.0, "g"
	result := mealai.Result{State: mealai.StateReady, Items: []mealai.Item{
		{
			Mention: "200 g tavuk", Intent: foodintent.FoodIntent{Query: "tavuk", Quantity: &quantity, UnitHint: &unit},
			State:     mealai.ItemReady,
			Food:      &mealai.ResolvedFood{FoodID: 1, DisplayName: "Tavuk", CanonicalName: "Chicken"},
			Selection: &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 1, Grams: &foodamount.GramsSelection{Grams: 200}},
		},
		{
			Mention: "yumurta", Intent: foodintent.FoodIntent{Query: "yumurta"},
			State: mealai.ItemReady,
			Food:  &mealai.ResolvedFood{FoodID: 2, DisplayName: "Yumurta", CanonicalName: "Egg"},
			Selection: &foodamount.Selection{
				Kind: foodamount.SelectionPortion, FoodID: 2,
				Portion: &foodamount.PortionSelection{PortionID: 9, Quantity: 2, Amount: 1, Measure: "adet", PortionGrams: 50},
			},
		},
	}}
	response := performRequestWithBody(mealRouter(&stubMealTextInterpreter{result: result}), http.MethodPost, "/ai/meals/interpret", `{"text":"meal"}`)
	if response.Code != 200 {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, fragment := range []string{
		`"brand":null`, `"clarification":null`,
		`"quantity":null`, `"unit_hint":null`,
		`"kind":"grams","food_id":1,"grams":200,"portion":null`,
		`"kind":"portion","food_id":2,"grams":null,"portion":{"portion_id":9,"quantity":2,"amount":1,"measure":"adet","portion_grams":50}`,
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("response %q missing %q", body, fragment)
		}
	}
	if strings.Contains(body, "resolved_grams") {
		t.Fatal("portion response exposed resolved_grams")
	}
}

func TestMealInterpretSerializesFoodClarificationInCandidateOrder(t *testing.T) {
	t.Parallel()

	brand := "Second Brand"
	result := mealai.Result{State: mealai.StateClarificationRequired, Items: []mealai.Item{{
		Mention: "elma", Intent: foodintent.FoodIntent{Query: "elma"}, State: mealai.ItemClarificationRequired,
		Clarification: &mealai.Clarification{
			Kind: mealai.ClarificationFoodIdentity, Reason: "multiple_exact_identities",
			Candidates: []mealai.FoodOption{
				{FoodID: 2, DisplayName: "İkinci", CanonicalName: "Second", Brand: &brand},
				{FoodID: 1, DisplayName: "Birinci", CanonicalName: "First"},
			},
			Portions: []food.Portion{}, AllowDirectGrams: false,
		},
	}}}
	response := performRequestWithBody(mealRouter(&stubMealTextInterpreter{result: result}), http.MethodPost, "/ai/meals/interpret", `{"text":"elma"}`)
	if response.Code != 200 {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Index(body, `"food_id":2`) > strings.Index(body, `"food_id":1`) {
		t.Fatalf("candidate order changed: %s", body)
	}
	for _, fragment := range []string{`"food":null`, `"selection":null`, `"portions":[]`, `"allow_direct_grams":false`} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("response %q missing %q", body, fragment)
		}
	}
}

func TestMealInterpretSerializesAmountClarificationInPortionOrder(t *testing.T) {
	t.Parallel()

	result := mealai.Result{State: mealai.StateClarificationRequired, Items: []mealai.Item{{
		Mention: "2 yumurta", Intent: foodintent.FoodIntent{Query: "yumurta", Quantity: floatPointerHTTP(2), UnitHint: stringPointerHTTP("adet")},
		State: mealai.ItemClarificationRequired,
		Food:  &mealai.ResolvedFood{FoodID: 3, DisplayName: "Yumurta", CanonicalName: "Egg"},
		Clarification: &mealai.Clarification{
			Kind: mealai.ClarificationAmount, Reason: "unsupported_unit_requires_clarification",
			Candidates: []mealai.FoodOption{},
			Portions: []food.Portion{
				{ID: 8, FoodID: 3, Amount: 1, Measure: "large", Grams: 60},
				{ID: 7, FoodID: 3, Amount: 1, Measure: "small", Grams: 40},
			},
			AllowDirectGrams: true,
		},
	}}}
	response := performRequestWithBody(mealRouter(&stubMealTextInterpreter{result: result}), http.MethodPost, "/ai/meals/interpret", `{"text":"2 yumurta"}`)
	if response.Code != 200 {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Index(body, `"portion_id":8`) > strings.Index(body, `"portion_id":7`) {
		t.Fatalf("portion order changed: %s", body)
	}
	for _, fragment := range []string{`"selection":null`, `"candidates":[]`, `"allow_direct_grams":true`} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("response %q missing %q", body, fragment)
		}
	}

	var decoded map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestMealInterpretRejectsMalformedApplicationSuccess(t *testing.T) {
	t.Parallel()

	tests := []mealai.Result{
		{State: mealai.StateEmpty, Items: nil},
		{State: mealai.StateReady, Items: []mealai.Item{}},
		{State: mealai.State("future"), Items: []mealai.Item{}},
		{
			State: mealai.StateReady,
			Items: []mealai.Item{{
				State:     mealai.ItemReady,
				Food:      &mealai.ResolvedFood{FoodID: 1},
				Selection: &foodamount.Selection{Kind: foodamount.SelectionKind("future"), FoodID: 1},
			}},
		},
	}
	for _, result := range tests {
		response := performRequestWithBody(mealRouter(&stubMealTextInterpreter{result: result}), http.MethodPost, "/ai/meals/interpret", `{"text":"meal"}`)
		if response.Code != http.StatusInternalServerError || response.Body.String() != "{\"status\":\"internal_error\"}\n" {
			t.Errorf("result %#v response = %d %q", result, response.Code, response.Body.String())
		}
	}
}

func mealRouter(interpreter MealTextInterpreter) http.Handler {
	return NewRouter(
		discardLogger(), time.Second, func(context.Context) error { return nil },
		&stubFoodSearcher{}, nil, nil, interpreter,
	)
}

func floatPointerHTTP(value float64) *float64 { return &value }

func stringPointerHTTP(value string) *string { return &value }
