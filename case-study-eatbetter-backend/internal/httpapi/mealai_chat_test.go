package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealai"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealchat"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type stubMealChatService struct {
	result   mealai.ChatResult
	err      error
	calls    int
	requests []mealai.ChatRequest
}

type noopChatInterpreter struct{}

func (noopChatInterpreter) InterpretInitial(context.Context, string) (mealchat.InitialInterpretation, error) {
	return mealchat.InitialInterpretation{}, nil
}

func (noopChatInterpreter) InterpretContinuation(context.Context, mealchat.ContinuationRequest) (mealchat.ContinuationDecision, error) {
	return mealchat.ContinuationDecision{}, nil
}

func (s *stubMealChatService) Chat(_ context.Context, request mealai.ChatRequest) (mealai.ChatResult, error) {
	s.calls++
	s.requests = append(s.requests, request)
	return s.result, s.err
}

func TestMealChatEndpointInitialSuccessIsPostOnlyAndNoStore(t *testing.T) {
	intent := foodintent.FoodIntent{Query: "elma", Quantity: floatPointerHTTP(150), UnitHint: stringPointerHTTP("g")}
	active := (*int)(nil)
	service := &stubMealChatService{result: mealai.ChatResult{
		Purpose: mealai.ChatPurposeNutritionQuery, State: mealai.StateReady,
		Assistant: mealai.AssistantResponse{Kind: mealai.AssistantNutritionAnswer, Text: "150 g Elma için besin değerleri hazır."},
		Items: []mealai.Item{{
			Mention: "150 g elma", Intent: intent, State: mealai.ItemReady,
			Food:      &mealai.ResolvedFood{FoodID: 7, DisplayName: "Elma", CanonicalName: "Apple"},
			Selection: &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 7, Grams: &foodamount.GramsSelection{Grams: 150}},
			Preview:   &mealai.NutritionPreview{ResolvedGrams: 150},
		}},
		ActiveItemIndex: active,
		NextState: mealai.ConversationState{
			Version: mealai.ConversationVersion, Purpose: mealai.ChatPurposeNutritionQuery,
			Items: []mealai.ConversationItemState{{Position: 0, Evidence: "150 g elma", Intent: intent}},
		},
	}}
	router := chatRouter(service)
	request := httptest.NewRequest(http.MethodPost, "/ai/meals/chat", strings.NewReader(`{"message":"150 g elma kaç kalori?","locale":"tr-TR","state":null}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), `"purpose":"nutrition_query"`) || !strings.Contains(response.Body.String(), `"assistant":{"kind":"nutrition_answer"`) {
		t.Fatalf("response = %d %q %#v", response.Code, response.Body.String(), response.Header())
	}
	if service.calls != 1 || service.requests[0].State != nil {
		t.Fatalf("service calls/requests = %d/%#v", service.calls, service.requests)
	}

	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/ai/meals/chat", nil))
	if getResponse.Code != http.StatusMethodNotAllowed || getResponse.Header().Get("Allow") != http.MethodPost || getResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET response = %d %#v", getResponse.Code, getResponse.Header())
	}
}

func TestMealChatEndpointDecodesContinuationState(t *testing.T) {
	service := &stubMealChatService{result: validChatClarificationResult()}
	router := chatRouter(service)
	body := `{"message":"ızgara olan","locale":"tr","state":{"version":2,"purpose":"meal_logging","items":[{"position":0,"evidence":"ızgara tavuk","amount_evidence":"150 g tavuk","intent":{"query":"ızgara tavuk","quantity":150,"unit_hint":"g"},"food_choice_id":null,"amount_choice":null}],"active_item_index":0}}`
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/ai/meals/chat", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if service.calls != 1 || service.requests[0].State == nil || service.requests[0].State.ActiveItemIndex == nil || *service.requests[0].State.ActiveItemIndex != 0 {
		t.Fatalf("request = %#v", service.requests)
	}
	if service.requests[0].State.Items[0].AmountEvidence == nil || *service.requests[0].State.Items[0].AmountEvidence != "150 g tavuk" {
		t.Fatalf("amount evidence request = %#v", service.requests[0].State.Items[0])
	}
}

func TestMealChatEndpointRejectsUnsafeJSON(t *testing.T) {
	oversized := `{"message":"` + strings.Repeat("x", mealChatRequestBodyLimit) + `","locale":"tr","state":null}`
	for _, body := range []string{
		`{`,
		`{"message":"elma","locale":"tr","state":null,"extra":true}`,
		`{"message":"elma","locale":"tr","assistant":{"kind":"guidance","text":"unsafe"},"state":null}`,
		`{"message":"elma","locale":"tr","state":null}{"message":"ikinci"}`,
		`{"message":"elma","locale":"tr","state":{"version":1,"purpose":"meal_logging","items":[],"active_item_index":null,"extra":1}}`,
		oversized,
	} {
		service := &stubMealChatService{}
		response := httptest.NewRecorder()
		chatRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/ai/meals/chat", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest || service.calls != 0 {
			t.Fatalf("body prefix %q response/calls = %d/%d", body[:minHTTP(len(body), 30)], response.Code, service.calls)
		}
	}
}

func TestMealChatEndpointMapsErrorsAndMalformedOutputSafely(t *testing.T) {
	malformedAssistant := validChatClarificationResult()
	malformedAssistant.Assistant.Kind = mealai.AssistantMealReady
	for _, test := range []struct {
		service *stubMealChatService
		want    int
	}{
		{&stubMealChatService{err: &mealai.Error{Kind: mealai.ErrorInvalidInput}}, http.StatusBadRequest},
		{&stubMealChatService{err: &mealai.Error{Kind: mealai.ErrorAIRateLimited}}, http.StatusTooManyRequests},
		{&stubMealChatService{result: mealai.ChatResult{Purpose: mealai.ChatPurposeMealLogging, State: mealai.StateReady}}, http.StatusInternalServerError},
		{&stubMealChatService{result: malformedAssistant}, http.StatusInternalServerError},
	} {
		response := httptest.NewRecorder()
		chatRouter(test.service).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/ai/meals/chat", strings.NewReader(`{"message":"elma","locale":"tr","state":null}`)))
		if response.Code != test.want {
			t.Fatalf("response = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
		}
	}

	response := httptest.NewRecorder()
	chatRouter(nil).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/ai/meals/chat", strings.NewReader(`{"message":"elma","locale":"tr","state":null}`)))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("nil dependency response = %d", response.Code)
	}
}

func TestMealChatEndpointRejectsInvalidLocaleMessageAndState(t *testing.T) {
	application := mealai.NewService(nil, nil, nil, nil, nil, nil, noopChatInterpreter{})
	requests := []string{
		`{"message":"elma","locale":"tr_TR","state":null}`,
		`{"message":"   ","locale":"tr","state":null}`,
		`{"message":"cevap","locale":"tr","state":{"version":99,"purpose":"meal_logging","items":[{"position":0,"evidence":"elma","intent":{"query":"elma","quantity":null,"unit_hint":null},"food_choice_id":null,"amount_choice":null}],"active_item_index":0}}`,
	}
	for _, body := range requests {
		response := httptest.NewRecorder()
		chatRouter(application).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/ai/meals/chat", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s response = %d %s", body, response.Code, response.Body.String())
		}
	}
}

func validChatClarificationResult() mealai.ChatResult {
	active := 0
	intent := foodintent.FoodIntent{Query: "tavuk"}
	state := mealai.ConversationState{
		Version: mealai.ConversationVersion, Purpose: mealai.ChatPurposeMealLogging,
		Items: []mealai.ConversationItemState{{Position: 0, Evidence: "tavuk", Intent: intent}}, ActiveItemIndex: &active,
	}
	return mealai.ChatResult{
		Purpose: mealai.ChatPurposeMealLogging, State: mealai.StateClarificationRequired,
		Assistant: mealai.AssistantResponse{Kind: mealai.AssistantClarification, Text: "Bunu mu kastettin?"},
		Items: []mealai.Item{{Mention: "tavuk", Intent: intent, State: mealai.ItemClarificationRequired, Clarification: &mealai.Clarification{
			Kind: mealai.ClarificationFoodIdentity, Reason: "ambiguous", Candidates: []mealai.FoodOption{{FoodID: 1, DisplayName: "Tavuk", CanonicalName: "Chicken"}}, Portions: []food.Portion{},
		}}},
		ActiveItemIndex: &active, NextState: state,
	}
}

func chatRouter(chat MealChatService) http.Handler {
	return NewRouter(discardLogger(), time.Second, func(context.Context) error { return nil }, &stubFoodSearcher{}, nil, nil, nil, chat)
}

func minHTTP(left, right int) int {
	if left < right {
		return left
	}
	return right
}
