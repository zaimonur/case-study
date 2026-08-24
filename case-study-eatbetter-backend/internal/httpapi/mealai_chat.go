package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealai"
)

const mealChatRequestBodyLimit = 48 * 1024

// MealChatService is deliberately separate from the existing text/image and
// explicit-resolution HTTP boundary.
type MealChatService interface {
	Chat(context.Context, mealai.ChatRequest) (mealai.ChatResult, error)
}

var _ MealChatService = (*mealai.Service)(nil)

func mealChatHandler(logger *slog.Logger, service MealChatService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, mealChatRequestBodyLimit)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var command mealChatRequest
		if err := decoder.Decode(&command); err != nil {
			writeStatus(w, http.StatusBadRequest, "invalid_request")
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			writeStatus(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if service == nil {
			logger.ErrorContext(r.Context(), "meal chat dependency unavailable", "request_id", requestIDFromContext(r.Context()))
			writeStatus(w, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.Chat(r.Context(), mealai.ChatRequest{
			Message: command.Message, Locale: command.Locale, State: conversationState(command.State),
		})
		if err != nil {
			statusCode, status, kind := mealInterpretErrorResponse(err)
			logger.ErrorContext(r.Context(), "meal chat failed",
				"request_id", requestIDFromContext(r.Context()), "error_kind", kind)
			writeStatus(w, statusCode, status)
			return
		}
		response, err := mapMealChatResult(result)
		if err != nil {
			logger.ErrorContext(r.Context(), "meal chat result was invalid", "request_id", requestIDFromContext(r.Context()))
			writeStatus(w, http.StatusInternalServerError, "internal_error")
			return
		}
		ready, clarification := mealItemCounts(result.Items)
		logger.InfoContext(r.Context(), "meal chat completed",
			"request_id", requestIDFromContext(r.Context()), "purpose", result.Purpose,
			"state", result.State, "item_count", len(result.Items), "ready_count", ready,
			"clarification_count", clarification, "active_item_index", safeActiveIndex(result.ActiveItemIndex),
			"turn_kind", chatTurnKind(command.State), "assistant_kind", result.Assistant.Kind,
		)
		writeJSON(w, http.StatusOK, response)
	})
}

func mapMealChatResult(result mealai.ChatResult) (mealChatResponse, error) {
	if err := mealai.ValidateAssistantResponse(result); err != nil {
		return mealChatResponse{}, err
	}
	switch result.Purpose {
	case mealai.ChatPurposeMealLogging, mealai.ChatPurposeNutritionQuery, mealai.ChatPurposeUnknown:
	default:
		return mealChatResponse{}, fmt.Errorf("unknown chat purpose")
	}
	mapped, err := mapMealInterpretResult(mealai.Result{State: result.State, Items: result.Items})
	if err != nil {
		return mealChatResponse{}, err
	}
	if result.NextState.Version != mealai.ConversationVersion || result.NextState.Purpose != result.Purpose || len(result.NextState.Items) != len(result.Items) || !sameIndex(result.ActiveItemIndex, result.NextState.ActiveItemIndex) {
		return mealChatResponse{}, fmt.Errorf("chat next state does not match materialized result")
	}
	for index := range result.Items {
		if result.NextState.Items[index].Evidence != result.Items[index].Mention || !reflect.DeepEqual(result.NextState.Items[index].Intent, result.Items[index].Intent) {
			return mealChatResponse{}, fmt.Errorf("chat replay evidence does not match materialized result")
		}
	}
	if result.State == mealai.StateClarificationRequired {
		if result.ActiveItemIndex == nil || *result.ActiveItemIndex < 0 || *result.ActiveItemIndex >= len(result.Items) || result.Items[*result.ActiveItemIndex].State != mealai.ItemClarificationRequired {
			return mealChatResponse{}, fmt.Errorf("invalid active clarification")
		}
		for index := 0; index < *result.ActiveItemIndex; index++ {
			if result.Items[index].State == mealai.ItemClarificationRequired {
				return mealChatResponse{}, fmt.Errorf("active clarification is not first unresolved item")
			}
		}
	} else if result.ActiveItemIndex != nil {
		return mealChatResponse{}, fmt.Errorf("non-clarification result has active item")
	}
	state, err := mapConversationState(result.NextState)
	if err != nil {
		return mealChatResponse{}, err
	}
	return mealChatResponse{
		Purpose: string(result.Purpose), State: mapped.State,
		Assistant: assistantResponse{Kind: string(result.Assistant.Kind), Text: result.Assistant.Text}, Items: mapped.Items,
		ActiveItemIndex: result.ActiveItemIndex, NextState: state,
	}, nil
}

func conversationState(request *mealChatState) *mealai.ConversationState {
	if request == nil {
		return nil
	}
	items := make([]mealai.ConversationItemState, 0, len(request.Items))
	for _, item := range request.Items {
		var amountChoice *mealai.ExplicitChoice
		if item.AmountChoice != nil {
			amountChoice = &mealai.ExplicitChoice{
				Kind: mealai.ChoiceKind(item.AmountChoice.Kind), Grams: item.AmountChoice.Grams,
				PortionID: item.AmountChoice.PortionID, Quantity: item.AmountChoice.Quantity,
			}
		}
		items = append(items, mealai.ConversationItemState{
			Position: item.Position, Evidence: item.Evidence, AmountEvidence: item.AmountEvidence, Intent: mealIntent(item.Intent),
			FoodChoiceID: item.FoodChoiceID, AmountChoice: amountChoice,
		})
	}
	return &mealai.ConversationState{
		Version: request.Version, Purpose: mealai.ChatPurpose(request.Purpose),
		Items: items, ActiveItemIndex: request.ActiveItemIndex,
	}
}

func mapConversationState(state mealai.ConversationState) (mealChatState, error) {
	result := mealChatState{
		Version: state.Version, Purpose: string(state.Purpose), ActiveItemIndex: state.ActiveItemIndex,
		Items: make([]mealChatStateItem, 0, len(state.Items)),
	}
	for index, item := range state.Items {
		if item.Position != index || item.Evidence == "" || item.FoodChoiceID != nil && *item.FoodChoiceID <= 0 {
			return mealChatState{}, fmt.Errorf("invalid replay item")
		}
		mapped := mealChatStateItem{
			Position: item.Position, Evidence: item.Evidence, AmountEvidence: item.AmountEvidence,
			Intent:       mealIntentRequest{Query: item.Intent.Query, Quantity: item.Intent.Quantity, UnitHint: item.Intent.UnitHint},
			FoodChoiceID: item.FoodChoiceID,
		}
		if item.AmountChoice != nil {
			if !validChatStateChoice(*item.AmountChoice) {
				return mealChatState{}, fmt.Errorf("invalid replay amount choice")
			}
			mapped.AmountChoice = &mealChoiceRequest{
				Kind: string(item.AmountChoice.Kind), Grams: item.AmountChoice.Grams,
				PortionID: item.AmountChoice.PortionID, Quantity: item.AmountChoice.Quantity,
			}
		}
		result.Items = append(result.Items, mapped)
	}
	return result, nil
}

func validChatStateChoice(choice mealai.ExplicitChoice) bool {
	switch choice.Kind {
	case mealai.ChoiceGrams:
		return choice.Grams != nil && finitePositiveMeal(*choice.Grams) && choice.PortionID == nil && choice.Quantity == nil
	case mealai.ChoicePortion:
		return choice.Grams == nil && choice.PortionID != nil && *choice.PortionID > 0 && choice.Quantity != nil && finitePositiveMeal(*choice.Quantity)
	default:
		return false
	}
}

func sameIndex(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func safeActiveIndex(index *int) int {
	if index == nil {
		return -1
	}
	return *index
}

func chatTurnKind(state *mealChatState) string {
	if state == nil {
		return "initial"
	}
	return "continuation"
}

type mealChatRequest struct {
	Message string         `json:"message"`
	Locale  string         `json:"locale"`
	State   *mealChatState `json:"state"`
}

type mealChatResponse struct {
	Purpose         string                      `json:"purpose"`
	State           string                      `json:"state"`
	Assistant       assistantResponse           `json:"assistant"`
	Items           []mealInterpretItemResponse `json:"items"`
	ActiveItemIndex *int                        `json:"active_item_index"`
	NextState       mealChatState               `json:"next_state"`
}

type mealChatState struct {
	Version         int                 `json:"version"`
	Purpose         string              `json:"purpose"`
	Items           []mealChatStateItem `json:"items"`
	ActiveItemIndex *int                `json:"active_item_index"`
}

type mealChatStateItem struct {
	Position       int                `json:"position"`
	Evidence       string             `json:"evidence"`
	AmountEvidence *string            `json:"amount_evidence"`
	Intent         mealIntentRequest  `json:"intent"`
	FoodChoiceID   *int64             `json:"food_choice_id"`
	AmountChoice   *mealChoiceRequest `json:"amount_choice"`
}

type assistantResponse struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}
