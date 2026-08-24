package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"
)

const (
	defaultBaseURL = "http://localhost:8080"
	defaultTimeout = 20 * time.Second
	maxBodyBytes   = 256 * 1024
)

const (
	caseNutritionQuery     = "direct_nutrition_query"
	caseMealLogging        = "direct_meal_logging"
	caseIdentityRegression = "amount_continuation_identity_regression"
	caseFoodRephrase       = "food_identity_rephrase"
	caseMultiFood          = "multi_food"
	caseUnknown            = "irrelevant_unknown"
)

type failureReason string

const (
	reasonNone                    failureReason = ""
	reasonInvalidConfiguration    failureReason = "invalid_configuration"
	reasonTransportFailure        failureReason = "transport_failure"
	reasonUnexpectedHTTPStatus    failureReason = "unexpected_http_status"
	reasonApplicationError        failureReason = "application_error"
	reasonOversizedResponse       failureReason = "oversized_response"
	reasonMalformedResponse       failureReason = "malformed_response"
	reasonUnexpectedContract      failureReason = "unexpected_contract"
	reasonPrerequisiteUnavailable failureReason = "prerequisite_unavailable"
)

type caseResult struct {
	CaseID            string        `json:"case_id"`
	Required          bool          `json:"required"`
	Outcome           string        `json:"outcome"`
	HTTPStatus        int           `json:"http_status,omitempty"`
	Purpose           string        `json:"purpose,omitempty"`
	State             string        `json:"state,omitempty"`
	AssistantKind     string        `json:"assistant_kind,omitempty"`
	ItemCount         int           `json:"item_count,omitempty"`
	Reason            failureReason `json:"reason"`
	ApplicationStatus string        `json:"application_status,omitempty"`
}

type smokeReport struct {
	Cases          []caseResult `json:"cases"`
	Total          int          `json:"total"`
	Passed         int          `json:"passed"`
	Failed         int          `json:"failed"`
	Skipped        int          `json:"skipped"`
	RequiredFailed int          `json:"required_failed"`
	DurationMS     float64      `json:"duration_ms"`
}

type smokeClient struct {
	origin string
	client *http.Client
}

type requestFailure struct {
	reason            failureReason
	httpStatus        int
	applicationStatus string
}

type chatRequest struct {
	Message string     `json:"message"`
	Locale  string     `json:"locale"`
	State   *chatState `json:"state"`
}

type chatResponse struct {
	Purpose         string         `json:"purpose"`
	State           string         `json:"state"`
	Assistant       assistant      `json:"assistant"`
	Items           []responseItem `json:"items"`
	ActiveItemIndex *int           `json:"active_item_index"`
	NextState       chatState      `json:"next_state"`
}

type assistant struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type chatState struct {
	Version         int             `json:"version"`
	Purpose         string          `json:"purpose"`
	Items           []chatStateItem `json:"items"`
	ActiveItemIndex *int            `json:"active_item_index"`
}

type chatStateItem struct {
	Position       int            `json:"position"`
	Evidence       string         `json:"evidence"`
	AmountEvidence *string        `json:"amount_evidence"`
	Intent         intentPayload  `json:"intent"`
	FoodChoiceID   *int64         `json:"food_choice_id"`
	AmountChoice   *choicePayload `json:"amount_choice"`
}

type responseItem struct {
	Mention       string                `json:"mention"`
	Intent        intentPayload         `json:"intent"`
	State         string                `json:"state"`
	Food          *foodPayload          `json:"food"`
	Selection     *selectionPayload     `json:"selection"`
	Preview       *previewPayload       `json:"preview"`
	Clarification *clarificationPayload `json:"clarification"`
}

type intentPayload struct {
	Query    string   `json:"query"`
	Quantity *float64 `json:"quantity"`
	UnitHint *string  `json:"unit_hint"`
}

type choicePayload struct {
	Kind      string   `json:"kind"`
	Grams     *float64 `json:"grams"`
	PortionID *int64   `json:"portion_id"`
	Quantity  *float64 `json:"quantity"`
}

type foodPayload struct {
	FoodID        int64   `json:"food_id"`
	DisplayName   string  `json:"display_name"`
	CanonicalName string  `json:"canonical_name"`
	Brand         *string `json:"brand"`
}

type selectionPayload struct {
	Kind    string                   `json:"kind"`
	FoodID  int64                    `json:"food_id"`
	Grams   *float64                 `json:"grams"`
	Portion *portionSelectionPayload `json:"portion"`
}

type portionSelectionPayload struct {
	PortionID    int64   `json:"portion_id"`
	Quantity     float64 `json:"quantity"`
	Amount       float64 `json:"amount"`
	Measure      string  `json:"measure"`
	PortionGrams float64 `json:"portion_grams"`
}

type previewPayload struct {
	ResolvedGrams float64          `json:"resolved_grams"`
	Nutrition     nutritionPayload `json:"nutrition"`
}

type nutritionPayload struct {
	Calories      *float64 `json:"calories_kcal"`
	Protein       *float64 `json:"protein_g"`
	Carbohydrates *float64 `json:"carbohydrates_g"`
	Fat           *float64 `json:"fat_g"`
}

type clarificationPayload struct {
	Kind             string             `json:"kind"`
	Reason           string             `json:"reason"`
	Candidates       []candidatePayload `json:"candidates"`
	Portions         []portionPayload   `json:"portions"`
	AllowDirectGrams bool               `json:"allow_direct_grams"`
}

type candidatePayload struct {
	FoodID        int64   `json:"food_id"`
	DisplayName   string  `json:"display_name"`
	CanonicalName string  `json:"canonical_name"`
	Brand         *string `json:"brand"`
}

type portionPayload struct {
	PortionID int64   `json:"portion_id"`
	Amount    float64 `json:"amount"`
	Measure   string  `json:"measure"`
	Grams     float64 `json:"grams"`
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("meal-ai-chat-smoke", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baseURL := flags.String("base-url", defaultBaseURL, "EatBetter API origin")
	timeout := flags.Duration("timeout", defaultTimeout, "per-request timeout")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return writeReport(stdout, stderr, configurationFailureReport())
	}
	origin, err := normalizeOrigin(*baseURL)
	if err != nil || *timeout <= 0 {
		return writeReport(stdout, stderr, configurationFailureReport())
	}
	report := executeSmoke(&smokeClient{origin: origin, client: &http.Client{Timeout: *timeout}})
	if json.NewEncoder(stdout).Encode(report) != nil {
		_, _ = fmt.Fprintln(stderr, "unable to write chat smoke report")
		return 1
	}
	if report.RequiredFailed != 0 {
		return 1
	}
	return 0
}

func writeReport(stdout, stderr io.Writer, report smokeReport) int {
	if json.NewEncoder(stdout).Encode(report) != nil {
		_, _ = fmt.Fprintln(stderr, "unable to write chat smoke report")
	}
	return 1
}

func normalizeOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		strings.Contains(raw, "#") || parsed.RawPath != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("invalid origin")
	}
	parsed.Path = ""
	return parsed.String(), nil
}

func executeSmoke(client *smokeClient) smokeReport {
	started := time.Now()
	cases := make([]caseResult, 0, 6)

	cases = append(cases, client.singleCase(
		caseNutritionQuery, "150 g az yağlı krem peynir kaç kalori?", "nutrition_query", "ready", "nutrition_answer",
		func(response chatResponse) failureReason { return validateTargetGrams(response.Items, 150) },
	))
	cases = append(cases, client.singleCase(
		caseMealLogging, "150 g az yağlı krem peynir yedim.", "meal_logging", "ready", "meal_ready",
		func(response chatResponse) failureReason { return validateTargetGrams(response.Items, 150) },
	))
	cases = append(cases, client.identityRegressionCase())
	cases = append(cases, skippedCase(caseFoodRephrase))

	multiInput := "2 yumurta ve 200 g tavuk yedim."
	cases = append(cases, client.singleCase(
		caseMultiFood, multiInput, "meal_logging", "", "",
		func(response chatResponse) failureReason {
			if len(response.Items) < 2 || !sourceOrderPreserved(response.Items, multiInput) {
				return reasonUnexpectedContract
			}
			return reasonNone
		},
	))
	cases = append(cases, client.singleCase(
		caseUnknown, "Bugün hava nasıl?", "unknown", "empty", "guidance", nil,
	))

	report := smokeReport{Cases: cases, Total: len(cases), DurationMS: float64(time.Since(started).Microseconds()) / 1000}
	for _, result := range cases {
		switch result.Outcome {
		case "passed":
			report.Passed++
		case "failed":
			report.Failed++
			if result.Required {
				report.RequiredFailed++
			}
		case "skipped":
			report.Skipped++
		}
	}
	return report
}

func (client *smokeClient) singleCase(caseID, message, purpose, state, assistantKind string, additional func(chatResponse) failureReason) caseResult {
	response, status, failure := client.postChat(chatRequest{Message: message, Locale: "tr-TR", State: nil})
	if failure != nil {
		return requestFailedCase(caseID, true, failure)
	}
	result := resultFromResponse(caseID, true, status, response)
	if reason := validateCommon(response); reason != reasonNone || response.Purpose != purpose || state != "" && response.State != state || assistantKind != "" && response.Assistant.Kind != assistantKind {
		result.Outcome, result.Reason = "failed", reasonUnexpectedContract
		return result
	}
	if additional != nil {
		if reason := additional(response); reason != reasonNone {
			result.Outcome, result.Reason = "failed", reason
			return result
		}
	}
	result.Outcome = "passed"
	return result
}

func (client *smokeClient) identityRegressionCase() caseResult {
	const caseID = caseIdentityRegression
	initial, status, failure := client.postChat(chatRequest{Message: "Az yağlı krem peynir yedim.", Locale: "tr-TR", State: nil})
	if failure != nil {
		return requestFailedCase(caseID, true, failure)
	}
	result := resultFromResponse(caseID, true, status, initial)
	if validateCommon(initial) != reasonNone || initial.Purpose != "meal_logging" || initial.State != "clarification_required" || initial.Assistant.Kind != "clarification" || initial.ActiveItemIndex == nil {
		result.Outcome, result.Reason = "failed", reasonUnexpectedContract
		return result
	}
	active := initial.Items[*initial.ActiveItemIndex]
	if active.Clarification == nil || active.Clarification.Kind != "amount" || active.Food == nil || active.Food.FoodID <= 0 {
		result.Outcome, result.Reason = "failed", reasonUnexpectedContract
		return result
	}
	foodID := active.Food.FoodID
	exactState := initial.NextState
	continuation, continuationStatus, failure := client.postChat(chatRequest{Message: "150 g", Locale: "tr-TR", State: &exactState})
	if failure != nil {
		return requestFailedCase(caseID, true, failure)
	}
	result = resultFromResponse(caseID, true, continuationStatus, continuation)
	if validateCommon(continuation) != reasonNone || continuation.Purpose != "meal_logging" || continuation.State != "ready" || continuation.Assistant.Kind != "meal_ready" ||
		validateFoodIdentityAndGrams(continuation.Items, foodID, 150) != reasonNone {
		result.Outcome, result.Reason = "failed", reasonUnexpectedContract
		return result
	}
	result.Outcome = "passed"
	return result
}

func (client *smokeClient) postChat(payload chatRequest) (chatResponse, int, *requestFailure) {
	body, err := json.Marshal(payload)
	if err != nil {
		return chatResponse{}, 0, &requestFailure{reason: reasonInvalidConfiguration}
	}
	request, err := http.NewRequest(http.MethodPost, client.origin+"/ai/meals/chat", bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, 0, &requestFailure{reason: reasonInvalidConfiguration}
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return chatResponse{}, 0, &requestFailure{reason: reasonTransportFailure}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		return chatResponse{}, response.StatusCode, &requestFailure{reason: reasonTransportFailure, httpStatus: response.StatusCode}
	}
	if len(responseBody) > maxBodyBytes {
		return chatResponse{}, response.StatusCode, &requestFailure{reason: reasonOversizedResponse, httpStatus: response.StatusCode}
	}
	if response.StatusCode != http.StatusOK {
		return chatResponse{}, response.StatusCode, classifyApplicationFailure(response.StatusCode, responseBody)
	}
	var result chatResponse
	if err := decodeStrict(responseBody, &result); err != nil {
		return chatResponse{}, response.StatusCode, &requestFailure{reason: reasonMalformedResponse, httpStatus: response.StatusCode}
	}
	return result, response.StatusCode, nil
}

func classifyApplicationFailure(statusCode int, body []byte) *requestFailure {
	var response struct {
		Status string `json:"status"`
	}
	if err := decodeStrict(body, &response); err != nil || response.Status == "" {
		return &requestFailure{reason: reasonUnexpectedHTTPStatus, httpStatus: statusCode}
	}
	failure := &requestFailure{reason: reasonApplicationError, httpStatus: statusCode}
	if safeApplicationStatuses[response.Status] {
		failure.applicationStatus = response.Status
	}
	return failure
}

func decodeStrict(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

var safeApplicationStatuses = map[string]bool{
	"invalid_request": true, "ai_unavailable": true, "ai_rate_limited": true,
	"ai_timeout": true, "ai_invalid_response": true, "ai_provider_error": true,
	"food_not_found": true, "portion_not_found": true, "dependency_timeout": true,
	"request_canceled": true, "internal_error": true,
}

func validateCommon(response chatResponse) failureReason {
	if response.Items == nil || response.NextState.Items == nil || strings.TrimSpace(response.Assistant.Text) == "" ||
		response.NextState.Version != 2 || response.NextState.Purpose != response.Purpose || len(response.NextState.Items) != len(response.Items) ||
		!sameIndex(response.ActiveItemIndex, response.NextState.ActiveItemIndex) {
		return reasonUnexpectedContract
	}
	if !compatiblePurposeStateAssistant(response.Purpose, response.State, response.Assistant.Kind) {
		return reasonUnexpectedContract
	}
	for index, item := range response.Items {
		replay := response.NextState.Items[index]
		if replay.Position != index || strings.TrimSpace(item.Mention) == "" || strings.TrimSpace(item.Intent.Query) == "" ||
			replay.Evidence != item.Mention || !reflect.DeepEqual(replay.Intent, item.Intent) {
			return reasonUnexpectedContract
		}
		switch item.State {
		case "ready":
			if validateReadyItem(item) != reasonNone {
				return reasonUnexpectedContract
			}
		case "clarification_required":
			if validateClarificationItem(item) != reasonNone {
				return reasonUnexpectedContract
			}
		default:
			return reasonUnexpectedContract
		}
	}

	switch response.State {
	case "ready":
		if len(response.Items) == 0 || response.ActiveItemIndex != nil {
			return reasonUnexpectedContract
		}
		for _, item := range response.Items {
			if item.State != "ready" {
				return reasonUnexpectedContract
			}
		}
	case "clarification_required":
		if response.ActiveItemIndex == nil || *response.ActiveItemIndex < 0 || *response.ActiveItemIndex >= len(response.Items) || response.Items[*response.ActiveItemIndex].State != "clarification_required" {
			return reasonUnexpectedContract
		}
		for index := 0; index < *response.ActiveItemIndex; index++ {
			if response.Items[index].State == "clarification_required" {
				return reasonUnexpectedContract
			}
		}
	case "empty":
		if len(response.Items) != 0 || response.ActiveItemIndex != nil {
			return reasonUnexpectedContract
		}
	default:
		return reasonUnexpectedContract
	}
	return reasonNone
}

func compatiblePurposeStateAssistant(purpose, state, kind string) bool {
	switch purpose {
	case "meal_logging":
		return state == "ready" && kind == "meal_ready" || state == "clarification_required" && kind == "clarification"
	case "nutrition_query":
		return state == "ready" && kind == "nutrition_answer" || state == "clarification_required" && kind == "clarification"
	case "unknown":
		return state == "empty" && kind == "guidance"
	default:
		return false
	}
}

func validateReadyItem(item responseItem) failureReason {
	if item.Food == nil || item.Food.FoodID <= 0 || strings.TrimSpace(item.Food.DisplayName) == "" || strings.TrimSpace(item.Food.CanonicalName) == "" ||
		item.Selection == nil || item.Selection.FoodID != item.Food.FoodID || item.Preview == nil || !finitePositive(item.Preview.ResolvedGrams) || item.Clarification != nil {
		return reasonUnexpectedContract
	}
	switch item.Selection.Kind {
	case "grams":
		if item.Selection.Grams == nil || !finitePositive(*item.Selection.Grams) || item.Selection.Portion != nil {
			return reasonUnexpectedContract
		}
	case "portion":
		portion := item.Selection.Portion
		if item.Selection.Grams != nil || portion == nil || portion.PortionID <= 0 || !finitePositive(portion.Quantity) ||
			!finitePositive(portion.Amount) || strings.TrimSpace(portion.Measure) == "" || !finitePositive(portion.PortionGrams) {
			return reasonUnexpectedContract
		}
	default:
		return reasonUnexpectedContract
	}
	return reasonNone
}

func validateClarificationItem(item responseItem) failureReason {
	if item.Selection != nil || item.Preview != nil || item.Clarification == nil || strings.TrimSpace(item.Clarification.Reason) == "" ||
		item.Clarification.Candidates == nil || item.Clarification.Portions == nil {
		return reasonUnexpectedContract
	}
	switch item.Clarification.Kind {
	case "amount":
		if item.Food == nil || item.Food.FoodID <= 0 || len(item.Clarification.Candidates) != 0 || !item.Clarification.AllowDirectGrams {
			return reasonUnexpectedContract
		}
	case "food_identity":
		if item.Food != nil || len(item.Clarification.Portions) != 0 || item.Clarification.AllowDirectGrams {
			return reasonUnexpectedContract
		}
		for _, candidate := range item.Clarification.Candidates {
			if candidate.FoodID <= 0 {
				return reasonUnexpectedContract
			}
		}
	default:
		return reasonUnexpectedContract
	}
	return reasonNone
}

func validateTargetGrams(items []responseItem, grams float64) failureReason {
	for _, item := range items {
		if item.Preview != nil && item.Food != nil && strings.EqualFold(strings.TrimSpace(item.Intent.Query), "az yağlı krem peynir") && approximately(item.Preview.ResolvedGrams, grams) {
			return reasonNone
		}
	}
	return reasonUnexpectedContract
}

func validateFoodIdentityAndGrams(items []responseItem, foodID int64, grams float64) failureReason {
	for _, item := range items {
		if item.Food != nil && item.Food.FoodID == foodID && item.Preview != nil && approximately(item.Preview.ResolvedGrams, grams) {
			return reasonNone
		}
	}
	return reasonUnexpectedContract
}

func sourceOrderPreserved(items []responseItem, source string) bool {
	lastEnd := 0
	for _, item := range items {
		relative := strings.Index(source[lastEnd:], item.Mention)
		if relative < 0 {
			return false
		}
		lastEnd += relative + len(item.Mention)
	}
	return true
}

func resultFromResponse(caseID string, required bool, status int, response chatResponse) caseResult {
	return caseResult{
		CaseID: caseID, Required: required, HTTPStatus: status, Purpose: safePurpose(response.Purpose),
		State: safeState(response.State), AssistantKind: safeAssistantKind(response.Assistant.Kind), ItemCount: len(response.Items),
	}
}

func requestFailedCase(caseID string, required bool, failure *requestFailure) caseResult {
	return caseResult{
		CaseID: caseID, Required: required, Outcome: "failed", HTTPStatus: failure.httpStatus,
		Reason: failure.reason, ApplicationStatus: failure.applicationStatus,
	}
}

func skippedCase(caseID string) caseResult {
	return caseResult{CaseID: caseID, Required: false, Outcome: "skipped", Reason: reasonPrerequisiteUnavailable}
}

func configurationFailureReport() smokeReport {
	result := caseResult{CaseID: "configuration", Required: true, Outcome: "failed", Reason: reasonInvalidConfiguration}
	return smokeReport{Cases: []caseResult{result}, Total: 1, Failed: 1, RequiredFailed: 1}
}

func safePurpose(value string) string {
	switch value {
	case "meal_logging", "nutrition_query", "unknown":
		return value
	default:
		return ""
	}
}

func safeState(value string) string {
	switch value {
	case "ready", "clarification_required", "empty":
		return value
	default:
		return ""
	}
}

func safeAssistantKind(value string) string {
	switch value {
	case "nutrition_answer", "meal_ready", "clarification", "guidance":
		return value
	default:
		return ""
	}
}

func sameIndex(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func finitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

func approximately(left, right float64) bool {
	return math.Abs(left-right) < 1e-9
}
