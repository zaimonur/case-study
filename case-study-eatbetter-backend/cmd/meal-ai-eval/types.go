package main

import "encoding/json"

const (
	datasetVersion       = "mealai-chat-v1"
	frozenTask6AHead     = "862ab8d1dcbcc34e7e28a20f2e52265f9b6bae70"
	frozenDatasetSHA256  = "1f01eac070de57bf6bb73cb42e31cf3695642111ec3fc4db1f48080239f5b5d9"
	conversationVersion  = 2
	gramsTolerance       = 0.000001
	maxResponseBodyBytes = 512 * 1024
)

type evaluationCase struct {
	ID       string           `json:"id"`
	Category string           `json:"category"`
	Tags     []string         `json:"tags"`
	Locale   string           `json:"locale"`
	Turns    []evaluationTurn `json:"turns"`
	Notes    string           `json:"notes"`
}

type evaluationTurn struct {
	Message string       `json:"message"`
	Expect  *expectation `json:"expect"`
}

type expectation struct {
	Purpose            string         `json:"purpose"`
	State              string         `json:"state"`
	ClarificationKind  string         `json:"clarification_kind"`
	ActiveItemIndex    *int           `json:"active_item_index,omitempty"`
	MustNotAutoResolve *bool          `json:"must_not_auto_resolve"`
	Items              []expectedItem `json:"items"`
}

type expectedItem struct {
	SourceOrder           *int     `json:"source_order"`
	ExpectedFoodID        *int64   `json:"expected_food_id,omitempty"`
	AllowedFoodIDs        []int64  `json:"allowed_food_ids,omitempty"`
	ExpectedCanonicalName string   `json:"expected_canonical_name,omitempty"`
	ExpectedSource        string   `json:"expected_source,omitempty"`
	ExpectedExternalID    string   `json:"expected_external_id,omitempty"`
	ExpectedResolvedGrams *float64 `json:"expected_resolved_grams,omitempty"`
}

type chatRequest struct {
	Message string           `json:"message"`
	Locale  string           `json:"locale"`
	State   *json.RawMessage `json:"state"`
}

type chatResponse struct {
	Purpose         string          `json:"purpose"`
	State           string          `json:"state"`
	Assistant       assistant       `json:"assistant"`
	Items           []responseItem  `json:"items"`
	ActiveItemIndex *int            `json:"active_item_index"`
	NextState       json.RawMessage `json:"next_state"`
}

type assistant struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
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

type choicePayload struct {
	Kind      string   `json:"kind"`
	Grams     *float64 `json:"grams"`
	PortionID *int64   `json:"portion_id"`
	Quantity  *float64 `json:"quantity"`
}

type metric struct {
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Percentage  *float64 `json:"percentage"`
}

type report struct {
	RunStatus                   string            `json:"run_status"`
	DatasetVersion              string            `json:"dataset_version"`
	DatasetSHA256               string            `json:"dataset_sha256"`
	DatasetCaseCount            int               `json:"dataset_case_count"`
	DatasetTurnCount            int               `json:"dataset_turn_count"`
	FrozenTask6AGitHead         string            `json:"frozen_task6a_git_head"`
	StartedAt                   string            `json:"started_at"`
	CompletedAt                 string            `json:"completed_at"`
	DurationMS                  float64           `json:"duration_ms"`
	APIOrigin                   string            `json:"api_origin"`
	ConversationVersion         int               `json:"conversation_version"`
	ModelLabel                  *string           `json:"model_label,omitempty"`
	TotalCases                  int               `json:"total_cases"`
	EvaluableCases              int               `json:"evaluable_cases"`
	InfraErrorCases             int               `json:"infra_error_cases"`
	InfraErrors                 []infraDiagnostic `json:"infra_errors"`
	ProductFailureCases         int               `json:"product_failure_cases"`
	CanonicalResolutionAccuracy metric            `json:"canonical_resolution_accuracy"`
	AmountAccuracy              metric            `json:"amount_accuracy"`
	ClarificationCorrectness    metric            `json:"clarification_correctness"`
	UnsafeAutoResolutionRate    metric            `json:"unsafe_auto_resolution_rate"`
	EndToEndSuccessRate         metric            `json:"end_to_end_success_rate"`
	Cases                       []caseDiagnostic  `json:"cases"`
	HarnessError                string            `json:"harness_error,omitempty"`
}

type caseDiagnostic struct {
	CaseID   string           `json:"case_id"`
	Category string           `json:"category"`
	Locale   string           `json:"locale"`
	Outcome  string           `json:"outcome"`
	Turns    []turnDiagnostic `json:"turns"`
}

type turnDiagnostic struct {
	TurnIndex             int              `json:"turn_index"`
	Outcome               string           `json:"outcome"`
	ActualPurpose         *string          `json:"actual_purpose"`
	ActualState           *string          `json:"actual_state"`
	ActualActiveItemIndex *int             `json:"actual_active_item_index"`
	AssertionFailures     []string         `json:"assertion_failures"`
	SanitizedErrorKind    string           `json:"sanitized_error_kind,omitempty"`
	Attempts              int              `json:"attempts,omitempty"`
	Items                 []itemDiagnostic `json:"items,omitempty"`
}

type itemDiagnostic struct {
	ItemIndex           int      `json:"item_index"`
	ActualFoodID        *int64   `json:"actual_food_id"`
	ActualResolvedGrams *float64 `json:"actual_resolved_grams"`
}

type infraDiagnostic struct {
	CaseID    string `json:"case_id"`
	TurnIndex int    `json:"turn_index"`
	Kind      string `json:"kind"`
	Attempts  int    `json:"attempts"`
	Retries   int    `json:"retries"`
}

type counters struct {
	CanonicalCorrect int
	CanonicalTotal   int
	AmountCorrect    int
	AmountTotal      int
	ClarifyCorrect   int
	ClarifyTotal     int
	UnsafeTotal      int
	UnsafeCount      int
	E2ECorrect       int
	E2ETotal         int
}
