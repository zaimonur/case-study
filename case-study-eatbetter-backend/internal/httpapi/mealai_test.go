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
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealai"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type stubMealTextInterpreter struct {
	request        mealai.Request
	result         mealai.Result
	err            error
	calls          int
	ctx            context.Context
	resolveRequest mealai.ResolveSelectionRequest
	resolveResult  mealai.ResolveSelectionResult
	resolveErr     error
	resolveCalls   int
	panicValue     any
}

type deterministicMealAmountResolver struct{}

func (*deterministicMealAmountResolver) Resolve(_ context.Context, request foodamount.Request) (foodamount.Resolution, error) {
	return foodamount.Resolution{
		State: foodamount.StateResolved, Reason: foodamount.ReasonExplicitGrams,
		Selection: &foodamount.Selection{
			Kind: foodamount.SelectionGrams, FoodID: request.FoodID,
			Grams: &foodamount.GramsSelection{Grams: *request.Intent.Quantity},
		},
	}, nil
}

func (*deterministicMealAmountResolver) ResolvePortionSelection(context.Context, foodamount.PortionSelectionRequest) (foodamount.Resolution, error) {
	return foodamount.Resolution{}, errors.New("unexpected portion selection")
}

func (stub *stubMealTextInterpreter) ResolveSelection(ctx context.Context, request mealai.ResolveSelectionRequest) (mealai.ResolveSelectionResult, error) {
	if stub.panicValue != nil {
		panic(stub.panicValue)
	}
	stub.resolveCalls++
	stub.ctx = ctx
	stub.resolveRequest = request
	return stub.resolveResult, stub.resolveErr
}

func (stub *stubMealTextInterpreter) InterpretText(ctx context.Context, request mealai.Request) (mealai.Result, error) {
	if stub.panicValue != nil {
		panic(stub.panicValue)
	}
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

	service := mealai.NewService(foodextraction.NewService(nil), nil, nil, nil, nil)
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
			Preview:   &mealai.NutritionPreview{ResolvedGrams: 200},
		},
		{
			Mention: "yumurta", Intent: foodintent.FoodIntent{Query: "yumurta"},
			State: mealai.ItemReady,
			Food:  &mealai.ResolvedFood{FoodID: 2, DisplayName: "Yumurta", CanonicalName: "Egg"},
			Selection: &foodamount.Selection{
				Kind: foodamount.SelectionPortion, FoodID: 2,
				Portion: &foodamount.PortionSelection{PortionID: 9, Quantity: 2, Amount: 1, Measure: "adet", PortionGrams: 50},
			},
			Preview: &mealai.NutritionPreview{ResolvedGrams: 100},
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
		`"preview":{"resolved_grams":200,"nutrition":{"calories_kcal":null,"protein_g":null,"carbohydrates_g":null,"fat_g":null}}`,
		`"preview":{"resolved_grams":100,"nutrition":{"calories_kcal":null,"protein_g":null,"carbohydrates_g":null,"fat_g":null}}`,
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("response %q missing %q", body, fragment)
		}
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
	for _, fragment := range []string{`"food":null`, `"selection":null`, `"preview":null`, `"portions":[]`, `"allow_direct_grams":false`} {
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
	for _, fragment := range []string{`"selection":null`, `"preview":null`, `"candidates":[]`, `"allow_direct_grams":true`} {
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

func TestMealResolveIsPostOnly(t *testing.T) {
	t.Parallel()

	response := performRequest(mealRouter(&stubMealTextInterpreter{}), http.MethodGet, "/ai/meals/resolve")
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("response = %d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestMealResolveForwardsExplicitNullShapeAndSerializesPreview(t *testing.T) {
	t.Parallel()

	zero, err := food.NewNutrientAmount(0)
	if err != nil {
		t.Fatal(err)
	}
	stub := &stubMealTextInterpreter{resolveResult: mealai.ResolveSelectionResult{
		Intent: foodintent.FoodIntent{Query: "elma"}, State: mealai.ItemReady,
		Food:      &mealai.ResolvedFood{FoodID: 7, DisplayName: "Elma", CanonicalName: "Apple"},
		Selection: &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 7, Grams: &foodamount.GramsSelection{Grams: 125}},
		Preview: &mealai.NutritionPreview{
			ResolvedGrams: 125,
			Nutrition:     nutritionPreviewWithCalories(zero),
		},
	}}
	body := `{"food_id":7,"locale":"TR-tr","intent":{"query":"elma","quantity":null,"unit_hint":null},"choice":{"kind":"grams","grams":125,"portion_id":null,"quantity":null}}`
	response := performRequestWithBody(mealRouter(stub), http.MethodPost, "/ai/meals/resolve", body)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if stub.resolveCalls != 1 || stub.resolveRequest.FoodID != 7 || stub.resolveRequest.Locale != "TR-tr" || stub.resolveRequest.Intent.Query != "elma" || stub.resolveRequest.Intent.Quantity != nil || stub.resolveRequest.Intent.UnitHint != nil || stub.resolveRequest.Choice.Kind != mealai.ChoiceGrams || stub.resolveRequest.Choice.Grams == nil || *stub.resolveRequest.Choice.Grams != 125 || stub.resolveRequest.Choice.PortionID != nil || stub.resolveRequest.Choice.Quantity != nil {
		t.Fatalf("resolve request = %#v", stub.resolveRequest)
	}
	if requestIDFromContext(stub.ctx) == "" || response.Header().Get(requestIDHeader) == "" {
		t.Fatal("request ID middleware did not apply")
	}
	responseBody := response.Body.String()
	for _, fragment := range []string{
		`"state":"ready"`, `"preview":{"resolved_grams":125`, `"calories_kcal":0`, `"protein_g":null`, `"clarification":null`,
	} {
		if !strings.Contains(responseBody, fragment) {
			t.Fatalf("response %q missing %q", responseBody, fragment)
		}
	}
	if strings.Contains(responseBody, `"mention"`) {
		t.Fatalf("continuation response contains mention: %s", responseBody)
	}
}

func TestMealResolveRejectsMalformedBoundedJSONBeforeApplication(t *testing.T) {
	t.Parallel()

	stub := &stubMealTextInterpreter{}
	tests := []string{
		`{`,
		`{"food_id":1,"locale":"tr","intent":{"query":"elma","quantity":null,"unit_hint":null},"choice":{"kind":"food_identity","grams":null,"portion_id":null,"quantity":null},"extra":true}`,
		`{"food_id":1,"locale":"tr","intent":{"query":"elma","quantity":null,"unit_hint":null},"choice":{"kind":"food_identity","grams":null,"portion_id":null,"quantity":null}} {}`,
		`{"food_id":1,"locale":"tr","intent":{"query":"` + strings.Repeat("x", mealResolveRequestBodyLimit) + `","quantity":null,"unit_hint":null},"choice":{"kind":"food_identity","grams":null,"portion_id":null,"quantity":null}}`,
	}
	for _, body := range tests {
		response := performRequestWithBody(mealRouter(stub), http.MethodPost, "/ai/meals/resolve", body)
		if response.Code != http.StatusBadRequest || response.Body.String() != "{\"status\":\"invalid_request\"}\n" {
			t.Errorf("body length %d response = %d %q", len(body), response.Code, response.Body.String())
		}
	}
	if stub.resolveCalls != 0 {
		t.Fatalf("resolve calls = %d, want 0", stub.resolveCalls)
	}
}

func TestMealResolveMapsNotFoundErrors(t *testing.T) {
	t.Parallel()

	body := `{"food_id":7,"locale":"tr","intent":{"query":"elma","quantity":null,"unit_hint":null},"choice":{"kind":"food_identity","grams":null,"portion_id":null,"quantity":null}}`
	for _, test := range []struct {
		err        error
		statusCode int
		status     string
	}{
		{err: &mealai.Error{Kind: mealai.ErrorFoodNotFound}, statusCode: http.StatusNotFound, status: "food_not_found"},
		{err: &mealai.Error{Kind: mealai.ErrorPortionNotFound}, statusCode: http.StatusNotFound, status: "portion_not_found"},
	} {
		response := performRequestWithBody(mealRouter(&stubMealTextInterpreter{resolveErr: test.err}), http.MethodPost, "/ai/meals/resolve", body)
		if response.Code != test.statusCode || response.Body.String() != "{\"status\":\""+test.status+"\"}\n" {
			t.Errorf("error %v response = %d %q", test.err, response.Code, response.Body.String())
		}
	}
}

func TestMealResolveWorksWithoutConfiguredGroqExtractor(t *testing.T) {
	t.Parallel()

	detailer := &stubFoodDetailer{detail: fooddetail.Detail{
		Food: food.Food{ID: 7, CanonicalName: "Apple"}, DisplayName: "Elma", Portions: []food.Portion{},
	}}
	calculator := &stubNutritionCalculator{result: nutritioncalc.Result{FoodID: 7, ResolvedGrams: 50}}
	service := mealai.NewService(
		foodextraction.NewService(nil), nil, &deterministicMealAmountResolver{}, detailer, calculator,
	)
	body := `{"food_id":7,"locale":"tr","intent":{"query":"elma","quantity":null,"unit_hint":null},"choice":{"kind":"grams","grams":50,"portion_id":null,"quantity":null}}`
	response := performRequestWithBody(mealRouter(service), http.MethodPost, "/ai/meals/resolve", body)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"resolved_grams":50`) {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestMealResolveRejectsFoodIdentityClarificationSuccess(t *testing.T) {
	t.Parallel()

	stub := &stubMealTextInterpreter{resolveResult: mealai.ResolveSelectionResult{
		Intent: foodintent.FoodIntent{Query: "elma"}, State: mealai.ItemClarificationRequired,
		Clarification: &mealai.Clarification{
			Kind: mealai.ClarificationFoodIdentity, Reason: "unexpected",
			Candidates: []mealai.FoodOption{}, Portions: []food.Portion{},
		},
	}}
	body := `{"food_id":7,"locale":"tr","intent":{"query":"elma","quantity":null,"unit_hint":null},"choice":{"kind":"food_identity","grams":null,"portion_id":null,"quantity":null}}`
	response := performRequestWithBody(mealRouter(stub), http.MethodPost, "/ai/meals/resolve", body)
	if response.Code != http.StatusInternalServerError || response.Body.String() != "{\"status\":\"internal_error\"}\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestAIRoutesAlwaysDisableCaching(t *testing.T) {
	t.Parallel()

	readyResolve := readyResolveResultHTTP()
	tests := []struct {
		name   string
		stub   *stubMealTextInterpreter
		method string
		path   string
		body   string
	}{
		{name: "interpret success", stub: &stubMealTextInterpreter{result: mealai.Result{State: mealai.StateEmpty, Items: []mealai.Item{}}}, method: http.MethodPost, path: "/ai/meals/interpret", body: `{"text":"none","locale":"tr"}`},
		{name: "interpret invalid", stub: &stubMealTextInterpreter{}, method: http.MethodPost, path: "/ai/meals/interpret", body: `{`},
		{name: "interpret method", stub: &stubMealTextInterpreter{}, method: http.MethodGet, path: "/ai/meals/interpret"},
		{name: "interpret failure", stub: &stubMealTextInterpreter{err: &mealai.Error{Kind: mealai.ErrorAIFailure}}, method: http.MethodPost, path: "/ai/meals/interpret", body: `{"text":"meal","locale":"tr"}`},
		{name: "resolve success", stub: &stubMealTextInterpreter{resolveResult: readyResolve}, method: http.MethodPost, path: "/ai/meals/resolve", body: validResolveBodyHTTP()},
		{name: "resolve invalid", stub: &stubMealTextInterpreter{}, method: http.MethodPost, path: "/ai/meals/resolve", body: `{`},
		{name: "resolve method", stub: &stubMealTextInterpreter{}, method: http.MethodGet, path: "/ai/meals/resolve"},
		{name: "resolve failure", stub: &stubMealTextInterpreter{resolveErr: &mealai.Error{Kind: mealai.ErrorFoodNotFound}}, method: http.MethodPost, path: "/ai/meals/resolve", body: validResolveBodyHTTP()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performRequestWithBody(mealRouter(test.stub), test.method, test.path, test.body)
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestMealInterpretSuccessLogIsAggregateOnly(t *testing.T) {
	t.Parallel()

	const (
		mealSentinel      = "PRIVATE_MEAL_SENTINEL"
		mentionSentinel   = "PRIVATE_MENTION_SENTINEL"
		querySentinel     = "PRIVATE_QUERY_SENTINEL"
		foodSentinel      = "PRIVATE_FOOD_SENTINEL"
		canonicalSentinel = "PRIVATE_CANONICAL_SENTINEL"
		brandSentinel     = "PRIVATE_BRAND_SENTINEL"
	)
	brand := brandSentinel
	result := mealai.Result{State: mealai.StateReady, Items: []mealai.Item{{
		Mention: mentionSentinel, Intent: foodintent.FoodIntent{Query: querySentinel}, State: mealai.ItemReady,
		Food: &mealai.ResolvedFood{FoodID: 987654321, DisplayName: foodSentinel, CanonicalName: canonicalSentinel, Brand: &brand},
		Selection: &foodamount.Selection{
			Kind: foodamount.SelectionGrams, FoodID: 987654321, Grams: &foodamount.GramsSelection{Grams: 876543.25},
		},
		Preview: &mealai.NutritionPreview{ResolvedGrams: 876543.25},
	}}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := NewRouter(logger, time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, nil, &stubMealTextInterpreter{result: result})
	response := performRequestWithBody(router, http.MethodPost, "/ai/meals/interpret", `{"text":"`+mealSentinel+`","locale":"tr"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	logText := logs.String()
	for _, expected := range []string{`"msg":"meal interpretation completed"`, `"state":"ready"`, `"item_count":1`, `"ready_count":1`, `"clarification_count":0`, `"request_id":`} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("logs %q missing %q", logText, expected)
		}
	}
	for _, sensitive := range []string{mealSentinel, mentionSentinel, querySentinel, foodSentinel, canonicalSentinel, brandSentinel, "987654321", "876543.25"} {
		if strings.Contains(logText, sensitive) {
			t.Fatalf("logs exposed %q", sensitive)
		}
	}
}

func TestMealResolveSuccessLogIsBounded(t *testing.T) {
	t.Parallel()

	const querySentinel = "PRIVATE_RESOLVE_QUERY_SENTINEL"
	result := readyResolveResultHTTP()
	result.Intent.Query = querySentinel
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := NewRouter(logger, time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, nil, &stubMealTextInterpreter{resolveResult: result})
	body := strings.Replace(validResolveBodyHTTP(), `"query":"elma"`, `"query":"`+querySentinel+`"`, 1)
	response := performRequestWithBody(router, http.MethodPost, "/ai/meals/resolve", body)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	logText := logs.String()
	for _, expected := range []string{`"msg":"meal selection resolved"`, `"state":"ready"`, `"choice_kind":"grams"`, `"has_preview":true`, `"request_id":`} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("logs %q missing %q", logText, expected)
		}
	}
	for _, sensitive := range []string{querySentinel, "987654321", "876543.25", "765432.1"} {
		if strings.Contains(logText, sensitive) {
			t.Fatalf("logs exposed %q", sensitive)
		}
	}
}

func TestPanicRecoveryDoesNotLogRecoveredPayload(t *testing.T) {
	t.Parallel()

	const panicSentinel = "PRIVATE_PANIC_PAYLOAD_SENTINEL"
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := NewRouter(logger, time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, nil, &stubMealTextInterpreter{panicValue: panicSentinel})
	response := performRequestWithBody(router, http.MethodPost, "/ai/meals/interpret", `{"text":"meal","locale":"tr"}`)
	if response.Code != http.StatusInternalServerError || response.Body.String() != "{\"status\":\"internal_error\"}\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	requestID := response.Header().Get(requestIDHeader)
	if requestID == "" || !strings.Contains(logs.String(), requestID) {
		t.Fatal("panic response and log were not request-ID correlated")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	if strings.Contains(logs.String(), panicSentinel) || strings.Contains(logs.String(), `"error"`) {
		t.Fatalf("panic payload leaked: %s", logs.String())
	}
}

func mealRouter(interpreter MealAIService) http.Handler {
	return NewRouter(
		discardLogger(), time.Second, func(context.Context) error { return nil },
		&stubFoodSearcher{}, nil, nil, interpreter,
	)
}

func floatPointerHTTP(value float64) *float64 { return &value }

func stringPointerHTTP(value string) *string { return &value }

func nutritionPreviewWithCalories(calories food.NutrientAmount) nutritioncalc.Nutrition {
	return nutritioncalc.Nutrition{Calories: calories}
}

func readyResolveResultHTTP() mealai.ResolveSelectionResult {
	return mealai.ResolveSelectionResult{
		Intent: foodintent.FoodIntent{Query: "elma"}, State: mealai.ItemReady,
		Food: &mealai.ResolvedFood{FoodID: 987654321, DisplayName: "Elma", CanonicalName: "Apple"},
		Selection: &foodamount.Selection{
			Kind: foodamount.SelectionGrams, FoodID: 987654321,
			Grams: &foodamount.GramsSelection{Grams: 876543.25},
		},
		Preview: &mealai.NutritionPreview{ResolvedGrams: 876543.25},
	}
}

func validResolveBodyHTTP() string {
	return `{"food_id":987654321,"locale":"tr","intent":{"query":"elma","quantity":null,"unit_hint":null},"choice":{"kind":"grams","grams":876543.25,"portion_id":null,"quantity":null}}`
}
