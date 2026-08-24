package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestExactNextStateForwarding(t *testing.T) {
	foodID := int64(7)
	initialExpect := expectation{
		Purpose: "meal_logging", State: "clarification_required", ClarificationKind: "amount",
		ActiveItemIndex: intPointer(0), MustNotAutoResolve: boolPointer(false),
		Items: []expectedItem{{SourceOrder: intPointer(0), ExpectedFoodID: &foodID}},
	}
	readyExpect := directExpectation(foodID, 150)
	currentCase := evaluationCase{
		ID: "continuation", Category: "test", Locale: "tr-TR", Tags: []string{"test"}, Notes: "test",
		Turns: []evaluationTurn{{Message: "food", Expect: &initialExpect}, {Message: "150 g", Expect: &readyExpect}},
	}
	initial := clarificationChat("amount", &foodID)
	var gotState json.RawMessage
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		var payload chatRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if requests == 1 {
			if payload.State != nil {
				t.Fatal("initial request leaked state")
			}
			writeChatResponse(t, writer, initial)
			return
		}
		if payload.State == nil {
			t.Fatal("continuation omitted state")
		}
		gotState = append(json.RawMessage(nil), (*payload.State)...)
		writeChatResponse(t, writer, readyChat("meal_logging", []int64{foodID}, []float64{150}))
	}))
	defer server.Close()

	runner := testEvaluator(server.URL, nil)
	report := runner.evaluate(context.Background(), []evaluationCase{currentCase}, testBase(1, 2))
	if requests != 2 || report.RunStatus != "COMPLETE" || report.EndToEndSuccessRate.Numerator != 1 {
		t.Fatalf("requests/report = %d / %#v", requests, report)
	}
	var want, got chatState
	if err := decodeStrict(initial.NextState, &want); err != nil {
		t.Fatal(err)
	}
	if err := decodeStrict(gotState, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("forwarded state = %#v, want %#v", got, want)
	}
}

func TestContinuationRetryPreservesPayloadAndHonorsRetryAfter(t *testing.T) {
	foodID := int64(7)
	initialExpect := expectation{
		Purpose: "meal_logging", State: "clarification_required", ClarificationKind: "amount",
		ActiveItemIndex: intPointer(0), MustNotAutoResolve: boolPointer(false),
		Items: []expectedItem{{SourceOrder: intPointer(0), ExpectedFoodID: &foodID}},
	}
	readyExpect := directExpectation(foodID, 150)
	currentCase := evaluationCase{
		ID: "retry", Category: "test", Locale: "tr-TR", Tags: []string{"test"}, Notes: "test",
		Turns: []evaluationTurn{{Message: "food", Expect: &initialExpect}, {Message: "150 g", Expect: &readyExpect}},
	}
	requests := 0
	var continuationBodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		switch requests {
		case 1:
			writeChatResponse(t, writer, clarificationChat("amount", &foodID))
		case 2:
			continuationBodies = append(continuationBodies, body)
			writer.Header().Set("Retry-After", "7")
			writeStatusResponse(t, writer, http.StatusTooManyRequests, "ai_rate_limited")
		case 3:
			continuationBodies = append(continuationBodies, body)
			writeChatResponse(t, writer, readyChat("meal_logging", []int64{foodID}, []float64{150}))
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	var waits []time.Duration
	runner := testEvaluator(server.URL, &waits)
	runner.maxRetries = 1
	report := runner.evaluate(context.Background(), []evaluationCase{currentCase}, testBase(1, 2))
	if report.RunStatus != "COMPLETE" || requests != 3 || len(continuationBodies) != 2 || !bytes.Equal(continuationBodies[0], continuationBodies[1]) {
		t.Fatalf("report/requests/bodies = %#v / %d / %d", report, requests, len(continuationBodies))
	}
	if len(waits) != 1 || waits[0] != 7*time.Second || report.Cases[0].Turns[1].Attempts != 2 {
		t.Fatalf("waits/turn = %#v / %#v", waits, report.Cases[0].Turns[1])
	}
}

func TestBoundedRateLimitRetryProducesIncompleteRun(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writeStatusResponse(t, writer, http.StatusTooManyRequests, "ai_rate_limited")
	}))
	defer server.Close()
	var waits []time.Duration
	runner := testEvaluator(server.URL, &waits)
	runner.maxRetries = 2
	runner.retryBackoff = time.Second
	report := runner.evaluate(context.Background(), []evaluationCase{oneTurnCase("infra", directExpectation(7, 150))}, testBase(1, 1))
	if requests != 3 || len(waits) != 2 || report.RunStatus != "INCOMPLETE" || report.InfraErrorCases != 1 ||
		report.CanonicalResolutionAccuracy.Denominator != 0 || report.EndToEndSuccessRate.Denominator != 0 {
		t.Fatalf("requests/waits/report = %d / %#v / %#v", requests, waits, report)
	}
	if len(report.InfraErrors) != 1 || report.InfraErrors[0].Attempts != 3 || report.InfraErrors[0].Retries != 2 {
		t.Fatalf("infra = %#v", report.InfraErrors)
	}
}

func TestRetryAfterParsingIsBoundedAndDistinguishesZero(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	if wait, ok := retryAfterDuration("0", now); !ok || wait != 0 {
		t.Fatalf("zero Retry-After = %s, %v", wait, ok)
	}
	if wait, ok := retryAfterDuration("999999999999999999", now); !ok || wait != maxRetryWait {
		t.Fatalf("large Retry-After = %s, %v", wait, ok)
	}
	if wait, ok := retryAfterDuration("3600", now); !ok || wait != maxRetryWait {
		t.Fatalf("capped Retry-After = %s, %v", wait, ok)
	}
	date := now.Add(12 * time.Second).Format(http.TimeFormat)
	if wait, ok := retryAfterDuration(date, now); !ok || wait != 12*time.Second {
		t.Fatalf("date Retry-After = %s, %v", wait, ok)
	}
}

func TestInfrastructureCaseExcludedAndProductFailureIncluded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload chatRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Message == "infra" {
			writeStatusResponse(t, writer, http.StatusServiceUnavailable, "ai_unavailable")
			return
		}
		writeChatResponse(t, writer, unknownChat())
	}))
	defer server.Close()
	infraCase := oneTurnCase("infra", directExpectation(7, 150))
	infraCase.Turns[0].Message = "infra"
	successCase := oneTurnCase("success", unknownExpectation())
	successCase.Turns[0].Message = "success"
	runner := testEvaluator(server.URL, nil)
	report := runner.evaluate(context.Background(), []evaluationCase{infraCase, successCase}, testBase(2, 2))
	if report.RunStatus != "INCOMPLETE" || report.EvaluableCases != 1 || report.InfraErrorCases != 1 ||
		report.ClarificationCorrectness.Denominator != 1 || report.ClarificationCorrectness.Numerator != 1 ||
		report.EndToEndSuccessRate.Denominator != 1 || report.EndToEndSuccessRate.Numerator != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestProductErrorAndMalformedResponseRemainAccuracyFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		kind    string
	}{
		{
			name: "ai invalid response",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writeStatusResponse(t, writer, http.StatusBadGateway, "ai_invalid_response")
			},
			kind: "ai_invalid_response",
		},
		{
			name: "malformed success response",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(`{"purpose":`))
			},
			kind: "malformed_api_response",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			runner := testEvaluator(server.URL, nil)
			runner.maxRetries = 2
			report := runner.evaluate(context.Background(), []evaluationCase{oneTurnCase("product", directExpectation(7, 150))}, testBase(1, 1))
			if report.RunStatus != "COMPLETE" || report.EvaluableCases != 1 || report.ProductFailureCases != 1 ||
				report.CanonicalResolutionAccuracy.Denominator != 1 || report.AmountAccuracy.Denominator != 1 ||
				report.ClarificationCorrectness.Denominator != 1 || report.EndToEndSuccessRate.Denominator != 1 ||
				report.Cases[0].Turns[0].SanitizedErrorKind != test.kind || report.Cases[0].Turns[0].Attempts != 1 {
				t.Fatalf("report = %#v", report)
			}
		})
	}
}

func TestEarlyTerminalBlocksLaterTurnAndKeepsAssertions(t *testing.T) {
	foodID := int64(7)
	first := expectation{
		Purpose: "meal_logging", State: "clarification_required", ClarificationKind: "amount",
		ActiveItemIndex: intPointer(0), MustNotAutoResolve: boolPointer(false),
		Items: []expectedItem{{SourceOrder: intPointer(0), ExpectedFoodID: &foodID}},
	}
	second := directExpectation(foodID, 150)
	currentCase := evaluationCase{
		ID: "blocked", Category: "test", Locale: "tr-TR", Tags: []string{"test"}, Notes: "test",
		Turns: []evaluationTurn{{Message: "food", Expect: &first}, {Message: "150 g", Expect: &second}},
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writeChatResponse(t, writer, readyChat("meal_logging", []int64{foodID}, []float64{100}))
	}))
	defer server.Close()
	runner := testEvaluator(server.URL, nil)
	report := runner.evaluate(context.Background(), []evaluationCase{currentCase}, testBase(1, 2))
	if requests != 1 || report.RunStatus != "COMPLETE" || report.ProductFailureCases != 1 ||
		len(report.Cases[0].Turns) != 2 || report.Cases[0].Turns[1].Outcome != "blocked_turn" ||
		report.CanonicalResolutionAccuracy.Denominator != 2 || report.AmountAccuracy.Denominator != 1 ||
		report.ClarificationCorrectness.Denominator != 2 || report.EndToEndSuccessRate.Numerator != 0 {
		t.Fatalf("requests/report = %d / %#v", requests, report)
	}
}

func TestPacingOccursOnlyBetweenLogicalRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeChatResponse(t, writer, unknownChat())
	}))
	defer server.Close()
	var waits []time.Duration
	runner := testEvaluator(server.URL, &waits)
	runner.caseDelay = 3 * time.Second
	report := runner.evaluate(context.Background(), []evaluationCase{
		oneTurnCase("one", unknownExpectation()), oneTurnCase("two", unknownExpectation()),
	}, testBase(2, 2))
	if report.RunStatus != "COMPLETE" || len(waits) != 1 || waits[0] != 3*time.Second {
		t.Fatalf("waits/report = %#v / %#v", waits, report)
	}
}

func TestOversizedResponseIsProductFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(bytes.Repeat([]byte("x"), maxResponseBodyBytes+1))
	}))
	defer server.Close()
	runner := testEvaluator(server.URL, nil)
	report := runner.evaluate(context.Background(), []evaluationCase{oneTurnCase("oversized", directExpectation(7, 150))}, testBase(1, 1))
	if report.RunStatus != "COMPLETE" || report.ProductFailureCases != 1 || report.Cases[0].Turns[0].SanitizedErrorKind != "oversized_api_response" {
		t.Fatalf("report = %#v", report)
	}
}

func TestHarnessFailureMakesRunInvalid(t *testing.T) {
	runner := testEvaluator("://invalid", nil)
	report := runner.evaluate(context.Background(), []evaluationCase{oneTurnCase("harness", unknownExpectation())}, testBase(1, 1))
	if report.RunStatus != "INVALID" || report.HarnessError != "request_construction" || report.Cases[0].Turns[0].Outcome != "harness_error" {
		t.Fatalf("report = %#v", report)
	}
}
