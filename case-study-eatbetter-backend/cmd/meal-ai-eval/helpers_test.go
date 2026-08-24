package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func boolPointer(value bool) *bool { return &value }
func intPointer(value int) *int    { return &value }

func directExpectation(foodID int64, grams float64) expectation {
	return expectation{
		Purpose: "meal_logging", State: "ready", ClarificationKind: "none",
		MustNotAutoResolve: boolPointer(false),
		Items:              []expectedItem{{SourceOrder: intPointer(0), ExpectedFoodID: int64Pointer(foodID), ExpectedResolvedGrams: floatPointer(grams)}},
	}
}

func unknownExpectation() expectation {
	return expectation{
		Purpose: "unknown", State: "empty", ClarificationKind: "none",
		MustNotAutoResolve: boolPointer(true), Items: []expectedItem{},
	}
}

func oneTurnCase(id string, expect expectation) evaluationCase {
	return evaluationCase{
		ID: id, Category: "test", Tags: []string{"test"}, Locale: "tr-TR", Notes: "test",
		Turns: []evaluationTurn{{Message: "test message", Expect: &expect}},
	}
}

func readyChat(purpose string, ids []int64, grams []float64) chatResponse {
	items := make([]responseItem, 0, len(ids))
	stateItems := make([]chatStateItem, 0, len(ids))
	for index, id := range ids {
		intent := intentPayload{Query: "food"}
		value := grams[index]
		items = append(items, responseItem{
			Mention: "food", Intent: intent, State: "ready",
			Food:      &foodPayload{FoodID: id, DisplayName: "Food", CanonicalName: "Food"},
			Selection: &selectionPayload{Kind: "grams", FoodID: id, Grams: floatPointer(value)},
			Preview:   &previewPayload{ResolvedGrams: value},
		})
		stateItems = append(stateItems, chatStateItem{Position: index, Evidence: "food", Intent: intent})
	}
	state := mustRawJSON(chatState{Version: 2, Purpose: purpose, Items: stateItems})
	return chatResponse{
		Purpose: purpose, State: "ready", Assistant: assistant{Kind: "meal_ready", Text: "ignored"},
		Items: items, NextState: state,
	}
}

func unknownChat() chatResponse {
	state := mustRawJSON(chatState{Version: 2, Purpose: "unknown", Items: []chatStateItem{}})
	return chatResponse{
		Purpose: "unknown", State: "empty", Assistant: assistant{Kind: "guidance", Text: "ignored"},
		Items: []responseItem{}, NextState: state,
	}
}

func clarificationChat(kind string, foodID *int64) chatResponse {
	active := 0
	intent := intentPayload{Query: "food"}
	item := responseItem{
		Mention: "food", Intent: intent, State: "clarification_required",
		Clarification: &clarificationPayload{
			Kind: kind, Reason: "needs input", Candidates: []candidatePayload{}, Portions: []portionPayload{},
		},
	}
	if foodID != nil {
		item.Food = &foodPayload{FoodID: *foodID, DisplayName: "Food", CanonicalName: "Food"}
		item.Clarification.AllowDirectGrams = true
	}
	state := mustRawJSON(chatState{
		Version: 2, Purpose: "meal_logging", ActiveItemIndex: &active,
		Items: []chatStateItem{{Position: 0, Evidence: "food", Intent: intent}},
	})
	return chatResponse{
		Purpose: "meal_logging", State: "clarification_required", Assistant: assistant{Kind: "clarification", Text: "ignored"},
		Items: []responseItem{item}, ActiveItemIndex: &active, NextState: state,
	}
}

func mustRawJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func writeChatResponse(t *testing.T, writer http.ResponseWriter, response chatResponse) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		t.Fatal(err)
	}
}

func writeStatusResponse(t *testing.T, writer http.ResponseWriter, statusCode int, status string) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	if err := json.NewEncoder(writer).Encode(map[string]string{"status": status}); err != nil {
		t.Fatal(err)
	}
}

func testEvaluator(origin string, waits *[]time.Duration) *evaluator {
	return &evaluator{
		origin: origin, client: &http.Client{Timeout: time.Second}, maxRetries: 0,
		wait: func(value time.Duration) {
			if waits != nil {
				*waits = append(*waits, value)
			}
		},
		now: time.Now,
	}
}

func testBase(caseCount, turnCount int) report {
	return report{
		DatasetVersion: datasetVersion, DatasetSHA256: frozenDatasetSHA256,
		DatasetCaseCount: caseCount, DatasetTurnCount: turnCount,
		FrozenTask6AGitHead: frozenTask6AHead, APIOrigin: "http://test",
		ConversationVersion: conversationVersion,
	}
}
