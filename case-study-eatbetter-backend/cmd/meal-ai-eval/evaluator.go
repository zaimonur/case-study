package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const maxRetryWait = 30 * time.Second

type evaluator struct {
	origin       string
	client       *http.Client
	caseDelay    time.Duration
	maxRetries   int
	retryBackoff time.Duration
	wait         func(time.Duration)
	now          func() time.Time
	hasRequested bool
}

type callFailure struct {
	class        string
	kind         string
	retryable    bool
	retryWait    time.Duration
	retryWaitSet bool
}

type callResult struct {
	response *chatResponse
	failure  *callFailure
	attempts int
}

type caseResult struct {
	diagnostic caseDiagnostic
	counters   counters
	infra      *infraDiagnostic
	harness    string
	passed     bool
}

func (e *evaluator) evaluate(ctx context.Context, cases []evaluationCase, base report) report {
	started := e.now().UTC()
	base.RunStatus = "COMPLETE"
	base.StartedAt = started.Format(time.RFC3339Nano)
	base.TotalCases = len(cases)
	base.InfraErrors = []infraDiagnostic{}
	base.Cases = make([]caseDiagnostic, 0, len(cases))

	var totals counters
	for _, currentCase := range cases {
		result := e.evaluateCase(ctx, currentCase)
		base.Cases = append(base.Cases, result.diagnostic)
		if result.harness != "" {
			base.RunStatus = "INVALID"
			base.HarnessError = result.harness
			break
		}
		if result.infra != nil {
			base.InfraErrorCases++
			base.InfraErrors = append(base.InfraErrors, *result.infra)
			continue
		}
		base.EvaluableCases++
		addCounters(&totals, result.counters)
		if !result.passed {
			base.ProductFailureCases++
		}
	}
	if base.RunStatus != "INVALID" && base.InfraErrorCases != 0 {
		base.RunStatus = "INCOMPLETE"
	}
	base.CanonicalResolutionAccuracy = newMetric(totals.CanonicalCorrect, totals.CanonicalTotal)
	base.AmountAccuracy = newMetric(totals.AmountCorrect, totals.AmountTotal)
	base.ClarificationCorrectness = newMetric(totals.ClarifyCorrect, totals.ClarifyTotal)
	base.UnsafeAutoResolutionRate = newMetric(totals.UnsafeCount, totals.UnsafeTotal)
	base.EndToEndSuccessRate = newMetric(totals.E2ECorrect, totals.E2ETotal)
	completed := e.now().UTC()
	base.CompletedAt = completed.Format(time.RFC3339Nano)
	base.DurationMS = float64(completed.Sub(started).Microseconds()) / 1000
	if base.DurationMS < 0 {
		base.DurationMS = 0
	}
	return base
}

func (e *evaluator) evaluateCase(ctx context.Context, currentCase evaluationCase) caseResult {
	result := caseResult{
		diagnostic: caseDiagnostic{
			CaseID: currentCase.ID, Category: currentCase.Category, Locale: currentCase.Locale,
			Outcome: "passed", Turns: make([]turnDiagnostic, 0, len(currentCase.Turns)),
		},
		passed: true,
	}
	var state *json.RawMessage
	blocked := false
	for turnIndex, turn := range currentCase.Turns {
		if blocked {
			diagnostic, turnCounters := scoreUnavailableTurn(turnIndex, *turn.Expect, "blocked_turn", "")
			result.diagnostic.Turns = append(result.diagnostic.Turns, diagnostic)
			addCounters(&result.counters, turnCounters)
			result.passed = false
			continue
		}

		e.waitBeforeLogicalRequest()
		call := e.postChat(ctx, chatRequest{Message: turn.Message, Locale: currentCase.Locale, State: state})
		if call.failure != nil {
			if call.failure.class == "harness" {
				result.diagnostic.Turns = append(result.diagnostic.Turns, turnDiagnostic{
					TurnIndex: turnIndex, Outcome: "harness_error", AssertionFailures: []string{},
					SanitizedErrorKind: call.failure.kind, Attempts: call.attempts,
				})
				result.diagnostic.Outcome = "failed"
				result.passed = false
				result.harness = call.failure.kind
				return result
			}
			if call.failure.class == "infrastructure" {
				diagnostic := turnDiagnostic{
					TurnIndex: turnIndex, Outcome: "infrastructure_error", AssertionFailures: []string{},
					SanitizedErrorKind: call.failure.kind, Attempts: call.attempts,
				}
				result.diagnostic.Turns = append(result.diagnostic.Turns, diagnostic)
				result.diagnostic.Outcome = "infrastructure_error"
				result.passed = false
				result.infra = &infraDiagnostic{
					CaseID: currentCase.ID, TurnIndex: turnIndex, Kind: call.failure.kind,
					Attempts: call.attempts, Retries: max(0, call.attempts-1),
				}
				return result
			}
			diagnostic, turnCounters := scoreUnavailableTurn(turnIndex, *turn.Expect, "product_error", call.failure.kind)
			diagnostic.Attempts = call.attempts
			result.diagnostic.Turns = append(result.diagnostic.Turns, diagnostic)
			addCounters(&result.counters, turnCounters)
			result.passed = false
			blocked = true
			continue
		}

		diagnostic, turnCounters := scoreResponse(turnIndex, *turn.Expect, *call.response)
		diagnostic.Attempts = call.attempts
		result.diagnostic.Turns = append(result.diagnostic.Turns, diagnostic)
		addCounters(&result.counters, turnCounters)
		if diagnostic.Outcome != "passed" {
			result.passed = false
		}

		if turnIndex+1 < len(currentCase.Turns) {
			if call.response.State != "clarification_required" {
				blocked = true
				continue
			}
			exactState := append(json.RawMessage(nil), call.response.NextState...)
			state = &exactState
		}
	}

	result.counters.E2ETotal = 1
	if result.passed {
		result.counters.E2ECorrect = 1
	} else {
		result.diagnostic.Outcome = "failed"
	}
	return result
}

func (e *evaluator) waitBeforeLogicalRequest() {
	if e.hasRequested && e.caseDelay > 0 {
		e.wait(e.caseDelay)
	}
	e.hasRequested = true
}

func (e *evaluator) postChat(ctx context.Context, payload chatRequest) callResult {
	body, err := json.Marshal(payload)
	if err != nil {
		return callResult{failure: &callFailure{class: "harness", kind: "request_encoding"}}
	}
	for attempt := 1; attempt <= e.maxRetries+1; attempt++ {
		response, failure := e.postAttempt(ctx, body)
		if failure == nil {
			return callResult{response: response, attempts: attempt}
		}
		if !failure.retryable || attempt > e.maxRetries {
			return callResult{failure: failure, attempts: attempt}
		}
		wait := failure.retryWait
		if !failure.retryWaitSet {
			wait = e.retryBackoff
		}
		if wait > maxRetryWait {
			wait = maxRetryWait
		}
		if wait > 0 {
			e.wait(wait)
		}
	}
	panic("unreachable retry loop")
}

func (e *evaluator) postAttempt(ctx context.Context, body []byte) (*chatResponse, *callFailure) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.origin+"/ai/meals/chat", bytes.NewReader(body))
	if err != nil {
		return nil, &callFailure{class: "harness", kind: "request_construction"}
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := e.client.Do(request)
	if err != nil {
		kind := "transport_failure"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = "request_timeout"
		}
		return nil, &callFailure{class: "infrastructure", kind: kind, retryable: true}
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes+1))
	if readErr != nil {
		return nil, &callFailure{class: "infrastructure", kind: "response_read_failure", retryable: true}
	}
	if len(responseBody) > maxResponseBodyBytes {
		return nil, &callFailure{class: "product", kind: "oversized_api_response"}
	}
	if response.StatusCode != http.StatusOK {
		failure := classifyApplicationResponse(responseBody)
		if failure.class == "infrastructure" {
			failure.retryWait, failure.retryWaitSet = retryAfterDuration(response.Header.Get("Retry-After"), e.now())
		}
		return nil, failure
	}
	var result chatResponse
	if err := decodeStrict(responseBody, &result); err != nil {
		return nil, &callFailure{class: "product", kind: "malformed_api_response"}
	}
	if err := validateChatResponse(result); err != nil {
		return nil, &callFailure{class: "product", kind: "invalid_api_contract"}
	}
	return &result, nil
}

func classifyApplicationResponse(body []byte) *callFailure {
	var statusResponse struct {
		Status string `json:"status"`
	}
	if err := decodeStrict(body, &statusResponse); err != nil || statusResponse.Status == "" {
		return &callFailure{class: "product", kind: "malformed_api_error"}
	}
	switch statusResponse.Status {
	case "ai_unavailable", "ai_rate_limited", "ai_timeout", "ai_provider_error", "dependency_timeout":
		return &callFailure{class: "infrastructure", kind: statusResponse.Status, retryable: true}
	case "ai_invalid_response", "invalid_request", "food_not_found", "portion_not_found", "request_canceled", "internal_error":
		return &callFailure{class: "product", kind: statusResponse.Status}
	default:
		return &callFailure{class: "product", kind: "unexpected_application_error"}
	}
}

func retryAfterDuration(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		if seconds >= int64(maxRetryWait/time.Second) {
			return maxRetryWait, true
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0, false
	}
	wait := when.Sub(now)
	if wait > maxRetryWait {
		return maxRetryWait, true
	}
	return wait, true
}

func validateChatResponse(response chatResponse) error {
	if !oneOf(response.Purpose, "meal_logging", "nutrition_query", "unknown") ||
		!oneOf(response.State, "ready", "clarification_required", "empty") || response.Items == nil || len(response.NextState) == 0 {
		return fmt.Errorf("invalid top-level response")
	}
	var state chatState
	if err := decodeStrict(response.NextState, &state); err != nil {
		return fmt.Errorf("invalid next state: %w", err)
	}
	if state.Version != conversationVersion || state.Purpose != response.Purpose || state.Items == nil ||
		len(state.Items) != len(response.Items) || !sameIndex(state.ActiveItemIndex, response.ActiveItemIndex) {
		return fmt.Errorf("next state mismatch")
	}
	switch response.State {
	case "ready":
		if len(response.Items) == 0 || response.ActiveItemIndex != nil || response.Purpose == "unknown" {
			return fmt.Errorf("invalid ready response")
		}
	case "clarification_required":
		if len(response.Items) == 0 || response.ActiveItemIndex == nil || *response.ActiveItemIndex < 0 ||
			*response.ActiveItemIndex >= len(response.Items) || response.Purpose == "unknown" {
			return fmt.Errorf("invalid clarification response")
		}
	case "empty":
		if len(response.Items) != 0 || response.ActiveItemIndex != nil || response.Purpose != "unknown" {
			return fmt.Errorf("invalid empty response")
		}
	}
	for index, item := range response.Items {
		replay := state.Items[index]
		if replay.Position != index || strings.TrimSpace(item.Mention) == "" || strings.TrimSpace(item.Intent.Query) == "" ||
			replay.Evidence != item.Mention || strings.TrimSpace(replay.Intent.Query) == "" ||
			!reflect.DeepEqual(replay.Intent, item.Intent) || replay.FoodChoiceID != nil && *replay.FoodChoiceID <= 0 ||
			replay.AmountChoice != nil && !validStateChoice(*replay.AmountChoice) {
			return fmt.Errorf("invalid response item replay")
		}
		switch item.State {
		case "ready":
			if item.Food == nil || item.Food.FoodID <= 0 || item.Selection == nil || item.Preview == nil ||
				item.Selection.FoodID != item.Food.FoodID || !validSelection(*item.Selection) ||
				!finitePositive(item.Preview.ResolvedGrams) || item.Clarification != nil {
				return fmt.Errorf("invalid ready item")
			}
		case "clarification_required":
			if item.Selection != nil || item.Preview != nil || item.Clarification == nil ||
				strings.TrimSpace(item.Clarification.Reason) == "" || item.Clarification.Candidates == nil ||
				item.Clarification.Portions == nil || !oneOf(item.Clarification.Kind, "amount", "food_identity") {
				return fmt.Errorf("invalid clarification item")
			}
			if item.Clarification.Kind == "amount" && (item.Food == nil || item.Food.FoodID <= 0 ||
				len(item.Clarification.Candidates) != 0 || !item.Clarification.AllowDirectGrams) {
				return fmt.Errorf("amount clarification has no food")
			}
			if item.Clarification.Kind == "food_identity" && (item.Food != nil || len(item.Clarification.Portions) != 0 ||
				item.Clarification.AllowDirectGrams) {
				return fmt.Errorf("identity clarification materialized food")
			}
			for _, candidate := range item.Clarification.Candidates {
				if candidate.FoodID <= 0 {
					return fmt.Errorf("invalid clarification candidate")
				}
			}
			for _, portion := range item.Clarification.Portions {
				if portion.PortionID <= 0 || !finitePositive(portion.Amount) || !finitePositive(portion.Grams) || strings.TrimSpace(portion.Measure) == "" {
					return fmt.Errorf("invalid clarification portion")
				}
			}
		default:
			return fmt.Errorf("invalid item state")
		}
	}
	if response.State == "ready" {
		for _, item := range response.Items {
			if item.State != "ready" {
				return fmt.Errorf("ready response has unresolved item")
			}
		}
	}
	if response.State == "clarification_required" && response.Items[*response.ActiveItemIndex].State != "clarification_required" {
		return fmt.Errorf("active item is not unresolved")
	}
	if response.State == "clarification_required" {
		for index := 0; index < *response.ActiveItemIndex; index++ {
			if response.Items[index].State == "clarification_required" {
				return fmt.Errorf("active item is not first unresolved")
			}
		}
	}
	return nil
}

func validSelection(selection selectionPayload) bool {
	if selection.FoodID <= 0 {
		return false
	}
	switch selection.Kind {
	case "grams":
		return selection.Grams != nil && finitePositive(*selection.Grams) && selection.Portion == nil
	case "portion":
		portion := selection.Portion
		return selection.Grams == nil && portion != nil && portion.PortionID > 0 &&
			finitePositive(portion.Quantity) && finitePositive(portion.Amount) &&
			finitePositive(portion.PortionGrams) && strings.TrimSpace(portion.Measure) != ""
	default:
		return false
	}
}

func validStateChoice(choice choicePayload) bool {
	switch choice.Kind {
	case "grams":
		return choice.Grams != nil && finitePositive(*choice.Grams) && choice.PortionID == nil && choice.Quantity == nil
	case "portion":
		return choice.Grams == nil && choice.PortionID != nil && *choice.PortionID > 0 &&
			choice.Quantity != nil && finitePositive(*choice.Quantity)
	default:
		return false
	}
}

func scoreResponse(turnIndex int, expect expectation, actual chatResponse) (turnDiagnostic, counters) {
	diagnostic := turnDiagnostic{
		TurnIndex: turnIndex, Outcome: "passed", ActualPurpose: stringPointer(actual.Purpose),
		ActualState: stringPointer(actual.State), ActualActiveItemIndex: cloneInt(actual.ActiveItemIndex),
		AssertionFailures: []string{}, Items: itemDiagnostics(actual.Items),
	}
	var result counters
	conversationCorrect := true
	if actual.Purpose != expect.Purpose {
		addFailure(&diagnostic, "purpose_mismatch")
		conversationCorrect = false
	}
	if actual.State != expect.State {
		addFailure(&diagnostic, "state_mismatch")
		conversationCorrect = false
	}
	if len(actual.Items) != len(expect.Items) {
		addFailure(&diagnostic, "item_count_mismatch")
		conversationCorrect = false
	}
	actualClarification := clarificationKind(actual)
	if actualClarification != expect.ClarificationKind {
		addFailure(&diagnostic, "clarification_kind_mismatch")
		conversationCorrect = false
	}
	if !sameIndex(actual.ActiveItemIndex, expect.ActiveItemIndex) {
		addFailure(&diagnostic, "active_item_mismatch")
		conversationCorrect = false
	}

	for itemIndex, expected := range expect.Items {
		var actualItem *responseItem
		if itemIndex < len(actual.Items) {
			actualItem = &actual.Items[itemIndex]
		}
		if hasExpectedIdentity(expected) {
			result.CanonicalTotal++
			if actualItem != nil && actualItem.Food != nil && expectedIdentityMatches(expected, actualItem.Food.FoodID) {
				result.CanonicalCorrect++
			} else {
				addFailure(&diagnostic, "food_id_mismatch")
				if expectedIdentityAppearsElsewhere(expected, actual.Items, itemIndex) {
					addFailure(&diagnostic, "source_order_mismatch")
					conversationCorrect = false
				}
			}
		}
		if expected.ExpectedResolvedGrams != nil {
			result.AmountTotal++
			if actualItem != nil && actualItem.Preview != nil && math.Abs(actualItem.Preview.ResolvedGrams-*expected.ExpectedResolvedGrams) <= gramsTolerance {
				result.AmountCorrect++
			} else {
				addFailure(&diagnostic, "amount_mismatch")
			}
		}
	}

	result.ClarifyTotal = 1
	if conversationCorrect {
		result.ClarifyCorrect = 1
	}
	if *expect.MustNotAutoResolve {
		result.UnsafeTotal = 1
		if actual.State == "ready" && hasMaterializedFood(actual.Items) {
			result.UnsafeCount = 1
			addFailure(&diagnostic, "unsafe_auto_resolution")
		}
	}
	if len(diagnostic.AssertionFailures) != 0 {
		diagnostic.Outcome = "failed"
	}
	return diagnostic, result
}

func scoreUnavailableTurn(turnIndex int, expect expectation, failure, errorKind string) (turnDiagnostic, counters) {
	diagnostic := turnDiagnostic{
		TurnIndex: turnIndex, Outcome: failure, AssertionFailures: []string{failure}, SanitizedErrorKind: errorKind,
	}
	var result counters
	result.ClarifyTotal = 1
	if *expect.MustNotAutoResolve {
		result.UnsafeTotal = 1
	}
	for _, item := range expect.Items {
		if hasExpectedIdentity(item) {
			result.CanonicalTotal++
			addFailure(&diagnostic, "food_id_mismatch")
		}
		if item.ExpectedResolvedGrams != nil {
			result.AmountTotal++
			addFailure(&diagnostic, "amount_mismatch")
		}
	}
	return diagnostic, result
}

func clarificationKind(actual chatResponse) string {
	if actual.State != "clarification_required" {
		return "none"
	}
	if actual.ActiveItemIndex == nil || *actual.ActiveItemIndex < 0 || *actual.ActiveItemIndex >= len(actual.Items) {
		return ""
	}
	clarification := actual.Items[*actual.ActiveItemIndex].Clarification
	if clarification == nil {
		return ""
	}
	return clarification.Kind
}

func expectedIdentityAppearsElsewhere(expected expectedItem, actual []responseItem, expectedIndex int) bool {
	for index, item := range actual {
		if index != expectedIndex && item.Food != nil && expectedIdentityMatches(expected, item.Food.FoodID) {
			return true
		}
	}
	return false
}

func hasExpectedIdentity(item expectedItem) bool {
	return item.ExpectedFoodID != nil || len(item.AllowedFoodIDs) != 0
}

func expectedIdentityMatches(expected expectedItem, actual int64) bool {
	if expected.ExpectedFoodID != nil {
		return actual == *expected.ExpectedFoodID
	}
	for _, allowed := range expected.AllowedFoodIDs {
		if actual == allowed {
			return true
		}
	}
	return false
}

func hasMaterializedFood(items []responseItem) bool {
	for _, item := range items {
		if item.Food != nil && item.Food.FoodID > 0 {
			return true
		}
	}
	return false
}

func itemDiagnostics(items []responseItem) []itemDiagnostic {
	result := make([]itemDiagnostic, 0, len(items))
	for index, item := range items {
		diagnostic := itemDiagnostic{ItemIndex: index}
		if item.Food != nil {
			diagnostic.ActualFoodID = int64Pointer(item.Food.FoodID)
		}
		if item.Preview != nil {
			diagnostic.ActualResolvedGrams = floatPointer(item.Preview.ResolvedGrams)
		}
		result = append(result, diagnostic)
	}
	return result
}

func addFailure(diagnostic *turnDiagnostic, failure string) {
	for _, existing := range diagnostic.AssertionFailures {
		if existing == failure {
			return
		}
	}
	diagnostic.AssertionFailures = append(diagnostic.AssertionFailures, failure)
}

func addCounters(total *counters, add counters) {
	total.CanonicalCorrect += add.CanonicalCorrect
	total.CanonicalTotal += add.CanonicalTotal
	total.AmountCorrect += add.AmountCorrect
	total.AmountTotal += add.AmountTotal
	total.ClarifyCorrect += add.ClarifyCorrect
	total.ClarifyTotal += add.ClarifyTotal
	total.UnsafeCount += add.UnsafeCount
	total.UnsafeTotal += add.UnsafeTotal
	total.E2ECorrect += add.E2ECorrect
	total.E2ETotal += add.E2ETotal
}

func newMetric(numerator, denominator int) metric {
	result := metric{Numerator: numerator, Denominator: denominator}
	if denominator != 0 {
		percentage := float64(numerator) * 100 / float64(denominator)
		result.Percentage = &percentage
	}
	return result
}

func sameIndex(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func stringPointer(value string) *string  { return &value }
func int64Pointer(value int64) *int64     { return &value }
func floatPointer(value float64) *float64 { return &value }
