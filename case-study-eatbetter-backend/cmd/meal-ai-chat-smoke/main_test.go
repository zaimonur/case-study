package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExecuteSmokeCoversRequiredChatCasesAndExactContinuationState(t *testing.T) {
	initial := amountClarificationResponse()
	expectedState := initial.NextState
	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Method != http.MethodPost || request.URL.Path != "/ai/meals/chat" || request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request = %s %s %#v", request.Method, request.URL.Path, request.Header)
		}
		var payload chatRequest
		decodeSmokeRequest(t, request.Body, &payload)
		var response chatResponse
		switch payload.Message {
		case "150 g az yağlı krem peynir kaç kalori?":
			response = readyResponse("nutrition_query", "nutrition_answer", "150 g az yağlı krem peynir", "az yağlı krem peynir", 42, 150)
		case "150 g az yağlı krem peynir yedim.":
			response = readyResponse("meal_logging", "meal_ready", "150 g az yağlı krem peynir", "az yağlı krem peynir", 42, 150)
		case "Az yağlı krem peynir yedim.":
			response = initial
		case "150 g":
			if payload.State == nil || !reflect.DeepEqual(*payload.State, expectedState) {
				t.Fatalf("continuation state = %#v, want %#v", payload.State, expectedState)
			}
			response = readyResponse("meal_logging", "meal_ready", "Az yağlı krem peynir", "az yağlı krem peynir", 42, 150)
			grams := 150.0
			foodID := int64(42)
			response.NextState.Items[0].FoodChoiceID = &foodID
			response.NextState.Items[0].AmountChoice = &choicePayload{Kind: "grams", Grams: &grams}
		case "2 yumurta ve 200 g tavuk yedim.":
			first := readyItem("2 yumurta", "yumurta", 21, 100)
			second := readyItem("200 g tavuk", "tavuk", 22, 200)
			response = responseForItems("meal_logging", "meal_ready", "ready", []responseItem{first, second}, nil)
		case "Bugün hava nasıl?":
			response = responseForItems("unknown", "guidance", "empty", []responseItem{}, nil)
		default:
			t.Fatalf("unexpected message %q", payload.Message)
		}
		return encodedHTTPResponse(t, http.StatusOK, response), nil
	})

	report := executeSmoke(&smokeClient{origin: "https://api.test", client: &http.Client{Timeout: time.Second, Transport: transport}})
	if report.Total != 6 || report.Passed != 5 || report.Failed != 0 || report.Skipped != 1 || report.RequiredFailed != 0 || requests != 6 {
		t.Fatalf("report/requests = %#v / %d", report, requests)
	}
	wantIDs := []string{caseNutritionQuery, caseMealLogging, caseIdentityRegression, caseFoodRephrase, caseMultiFood, caseUnknown}
	for index, want := range wantIDs {
		if report.Cases[index].CaseID != want {
			t.Fatalf("case %d = %q, want %q", index, report.Cases[index].CaseID, want)
		}
	}
	if report.Cases[3].Required || report.Cases[3].Outcome != "skipped" || report.Cases[3].Reason != reasonPrerequisiteUnavailable {
		t.Fatalf("optional case = %#v", report.Cases[3])
	}
}

func TestValidateCommonRejectsReplayAndActiveOrderingMismatches(t *testing.T) {
	valid := readyResponse("meal_logging", "meal_ready", "150 g elma", "elma", 7, 150)
	if reason := validateCommon(valid); reason != reasonNone {
		t.Fatalf("valid response reason = %q", reason)
	}

	replayMismatch := readyResponse("meal_logging", "meal_ready", "150 g elma", "elma", 7, 150)
	replayMismatch.NextState.Items[0].Evidence = "başka"
	if reason := validateCommon(replayMismatch); reason != reasonUnexpectedContract {
		t.Fatalf("replay mismatch reason = %q", reason)
	}

	firstActive := 1
	clarification := amountClarificationItem("elma", "elma", 7)
	orderingMismatch := responseForItems(
		"meal_logging", "clarification", "clarification_required",
		[]responseItem{clarification, clarification}, &firstActive,
	)
	if reason := validateCommon(orderingMismatch); reason != reasonUnexpectedContract {
		t.Fatalf("ordering mismatch reason = %q", reason)
	}
}

func TestRequiredFailuresAndReportsDoNotExposePayloadsOrSecrets(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":"ai_unavailable"}`)),
		}, nil
	})
	report := executeSmoke(&smokeClient{origin: "https://api.test", client: &http.Client{Timeout: time.Second, Transport: transport}})
	if report.RequiredFailed != 5 || report.Failed != 5 || report.Skipped != 1 {
		t.Fatalf("report = %#v", report)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{"az yağlı", "yumurta", "Bugün hava"} {
		if strings.Contains(string(encoded), sensitive) {
			t.Fatalf("report exposed request payload %q: %s", sensitive, encoded)
		}
	}

	var stdout, stderr bytes.Buffer
	secret := "do-not-print-this-secret"
	exitCode := runCLI([]string{"-base-url", "http://user:" + secret + "@localhost:8080"}, &stdout, &stderr)
	if exitCode != 1 || strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatalf("configuration output/exit = %q / %q / %d", stdout.String(), stderr.String(), exitCode)
	}
}

func readyResponse(purpose, kind, mention, query string, foodID int64, grams float64) chatResponse {
	return responseForItems(purpose, kind, "ready", []responseItem{readyItem(mention, query, foodID, grams)}, nil)
}

func readyItem(mention, query string, foodID int64, grams float64) responseItem {
	return responseItem{
		Mention: mention, Intent: intentPayload{Query: query}, State: "ready",
		Food:      &foodPayload{FoodID: foodID, DisplayName: strings.ToUpper(query[:1]) + query[1:], CanonicalName: "Canonical " + query},
		Selection: &selectionPayload{Kind: "grams", FoodID: foodID, Grams: &grams},
		Preview:   &previewPayload{ResolvedGrams: grams},
	}
}

func amountClarificationResponse() chatResponse {
	active := 0
	return responseForItems(
		"meal_logging", "clarification", "clarification_required",
		[]responseItem{amountClarificationItem("Az yağlı krem peynir", "az yağlı krem peynir", 42)}, &active,
	)
}

func amountClarificationItem(mention, query string, foodID int64) responseItem {
	return responseItem{
		Mention: mention, Intent: intentPayload{Query: query}, State: "clarification_required",
		Food: &foodPayload{FoodID: foodID, DisplayName: "Az yağlı krem peynir", CanonicalName: "Cheese, cream, low fat"},
		Clarification: &clarificationPayload{
			Kind: "amount", Reason: "quantity_required", Candidates: []candidatePayload{}, Portions: []portionPayload{}, AllowDirectGrams: true,
		},
	}
}

func responseForItems(purpose, kind, state string, items []responseItem, active *int) chatResponse {
	replay := make([]chatStateItem, 0, len(items))
	for index, item := range items {
		replayItem := chatStateItem{Position: index, Evidence: item.Mention, Intent: item.Intent}
		if item.Food != nil && item.State == "clarification_required" {
			foodID := item.Food.FoodID
			replayItem.FoodChoiceID = &foodID
		}
		replay = append(replay, replayItem)
	}
	return chatResponse{
		Purpose: purpose, State: state, Assistant: assistant{Kind: kind, Text: "Yanıt"}, Items: items, ActiveItemIndex: active,
		NextState: chatState{Version: 2, Purpose: purpose, Items: replay, ActiveItemIndex: active},
	}
}

func decodeSmokeRequest(t *testing.T, reader io.Reader, destination any) {
	t.Helper()
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func encodedHTTPResponse(t *testing.T, status int, response chatResponse) *http.Response {
	t.Helper()
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}
