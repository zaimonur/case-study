package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealai"
)

const (
	mealInterpretRequestBodyLimit = 32 * 1024
	mealResolveRequestBodyLimit   = 16 * 1024
)

// MealAIService is the initial and continuation meal AI application boundary.
type MealAIService interface {
	InterpretText(context.Context, mealai.Request) (mealai.Result, error)
	ResolveSelection(context.Context, mealai.ResolveSelectionRequest) (mealai.ResolveSelectionResult, error)
}

var _ MealAIService = (*mealai.Service)(nil)

func mealInterpretHandler(logger *slog.Logger, interpreter MealAIService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, mealInterpretRequestBodyLimit)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var command mealInterpretRequest
		if err := decoder.Decode(&command); err != nil {
			writeStatus(w, http.StatusBadRequest, "invalid_request")
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			writeStatus(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if interpreter == nil {
			logger.ErrorContext(r.Context(), "meal interpretation dependency unavailable",
				"request_id", requestIDFromContext(r.Context()))
			writeStatus(w, http.StatusInternalServerError, "internal_error")
			return
		}

		result, err := interpreter.InterpretText(r.Context(), mealai.Request{Text: command.Text, Locale: command.Locale})
		if err != nil {
			statusCode, status, kind := mealInterpretErrorResponse(err)
			logger.ErrorContext(r.Context(), "meal interpretation failed",
				"request_id", requestIDFromContext(r.Context()), "error_kind", kind)
			writeStatus(w, statusCode, status)
			return
		}
		response, err := mapMealInterpretResult(result)
		if err != nil {
			logger.ErrorContext(r.Context(), "meal interpretation result was invalid",
				"request_id", requestIDFromContext(r.Context()))
			writeStatus(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func mealResolveHandler(logger *slog.Logger, service MealAIService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, mealResolveRequestBodyLimit)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var command mealResolveRequest
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
			logger.ErrorContext(r.Context(), "meal selection dependency unavailable",
				"request_id", requestIDFromContext(r.Context()))
			writeStatus(w, http.StatusInternalServerError, "internal_error")
			return
		}

		result, err := service.ResolveSelection(r.Context(), mealai.ResolveSelectionRequest{
			FoodID: command.FoodID, Locale: command.Locale,
			Intent: mealIntent(command.Intent),
			Choice: mealai.ExplicitChoice{
				Kind: mealai.ChoiceKind(command.Choice.Kind), Grams: command.Choice.Grams,
				PortionID: command.Choice.PortionID, Quantity: command.Choice.Quantity,
			},
		})
		if err != nil {
			statusCode, status, kind := mealInterpretErrorResponse(err)
			logger.ErrorContext(r.Context(), "meal selection resolution failed",
				"request_id", requestIDFromContext(r.Context()), "error_kind", kind)
			writeStatus(w, statusCode, status)
			return
		}
		response, err := mapMealResolveResult(result)
		if err != nil {
			logger.ErrorContext(r.Context(), "meal selection result was invalid",
				"request_id", requestIDFromContext(r.Context()))
			writeStatus(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func mealInterpretErrorResponse(err error) (int, string, string) {
	var applicationError *mealai.Error
	kind := "unknown"
	if errors.As(err, &applicationError) {
		kind = string(applicationError.Kind)
	}
	switch {
	case mealai.IsKind(err, mealai.ErrorInvalidInput):
		return http.StatusBadRequest, "invalid_request", kind
	case mealai.IsKind(err, mealai.ErrorAIUnavailable):
		return http.StatusServiceUnavailable, "ai_unavailable", kind
	case mealai.IsKind(err, mealai.ErrorAIRateLimited):
		return http.StatusTooManyRequests, "ai_rate_limited", kind
	case mealai.IsKind(err, mealai.ErrorAITimeout):
		return http.StatusGatewayTimeout, "ai_timeout", kind
	case mealai.IsKind(err, mealai.ErrorAIInvalidResponse):
		return http.StatusBadGateway, "ai_invalid_response", kind
	case mealai.IsKind(err, mealai.ErrorAIFailure):
		return http.StatusBadGateway, "ai_provider_error", kind
	case mealai.IsKind(err, mealai.ErrorFoodNotFound):
		return http.StatusNotFound, "food_not_found", kind
	case mealai.IsKind(err, mealai.ErrorPortionNotFound):
		return http.StatusNotFound, "portion_not_found", kind
	case mealai.IsKind(err, mealai.ErrorTimeout):
		return http.StatusGatewayTimeout, "dependency_timeout", kind
	case mealai.IsKind(err, mealai.ErrorCanceled):
		return http.StatusRequestTimeout, "request_canceled", kind
	default:
		return http.StatusInternalServerError, "internal_error", kind
	}
}

func mapMealInterpretResult(result mealai.Result) (mealInterpretResponse, error) {
	if result.Items == nil {
		return mealInterpretResponse{}, fmt.Errorf("nil meal items")
	}
	items := make([]mealInterpretItemResponse, 0, len(result.Items))
	clarifications := 0
	for _, item := range result.Items {
		mapped, err := mapMealInterpretItem(item)
		if err != nil {
			return mealInterpretResponse{}, err
		}
		if item.State == mealai.ItemClarificationRequired {
			clarifications++
		}
		items = append(items, mapped)
	}
	switch result.State {
	case mealai.StateEmpty:
		if len(items) != 0 {
			return mealInterpretResponse{}, fmt.Errorf("empty result has items")
		}
	case mealai.StateReady:
		if len(items) == 0 || clarifications != 0 {
			return mealInterpretResponse{}, fmt.Errorf("malformed ready result")
		}
	case mealai.StateClarificationRequired:
		if len(items) == 0 || clarifications == 0 {
			return mealInterpretResponse{}, fmt.Errorf("malformed clarification result")
		}
	default:
		return mealInterpretResponse{}, fmt.Errorf("unknown meal result state")
	}
	return mealInterpretResponse{State: string(result.State), Items: items}, nil
}

func mapMealInterpretItem(item mealai.Item) (mealInterpretItemResponse, error) {
	response := mealInterpretItemResponse{
		Mention: item.Mention,
		Intent: mealIntentResponse{
			Query: item.Intent.Query, Quantity: item.Intent.Quantity, UnitHint: item.Intent.UnitHint,
		},
		State: string(item.State),
	}
	switch item.State {
	case mealai.ItemReady:
		if item.Food == nil || item.Selection == nil || item.Preview == nil || item.Clarification != nil {
			return mealInterpretItemResponse{}, fmt.Errorf("malformed ready item")
		}
		foodResponse, err := mapResolvedFood(item.Food)
		if err != nil {
			return mealInterpretItemResponse{}, err
		}
		selection, err := mapAmountSelection(item.Selection)
		if err != nil {
			return mealInterpretItemResponse{}, err
		}
		preview, err := mapNutritionPreview(item.Preview)
		if err != nil {
			return mealInterpretItemResponse{}, err
		}
		response.Food, response.Selection, response.Preview = foodResponse, selection, preview
	case mealai.ItemClarificationRequired:
		if item.Preview != nil {
			return mealInterpretItemResponse{}, fmt.Errorf("clarification has nutrition preview")
		}
		clarification, err := mapClarification(item)
		if err != nil {
			return mealInterpretItemResponse{}, err
		}
		response.Clarification = clarification
		if item.Food != nil {
			response.Food, err = mapResolvedFood(item.Food)
			if err != nil {
				return mealInterpretItemResponse{}, err
			}
		}
	default:
		return mealInterpretItemResponse{}, fmt.Errorf("unknown item state")
	}
	return response, nil
}

func mapMealResolveResult(result mealai.ResolveSelectionResult) (mealResolveResponse, error) {
	if result.Food == nil || (result.State == mealai.ItemClarificationRequired &&
		(result.Clarification == nil || result.Clarification.Kind != mealai.ClarificationAmount)) {
		return mealResolveResponse{}, fmt.Errorf("malformed continuation result")
	}
	item, err := mapMealInterpretItem(mealai.Item{
		Intent: result.Intent, State: result.State, Food: result.Food,
		Selection: result.Selection, Preview: result.Preview, Clarification: result.Clarification,
	})
	if err != nil {
		return mealResolveResponse{}, err
	}
	return mealResolveResponse{
		Intent: item.Intent, State: item.State, Food: item.Food,
		Selection: item.Selection, Preview: item.Preview, Clarification: item.Clarification,
	}, nil
}

func mapNutritionPreview(preview *mealai.NutritionPreview) (*nutritionPreviewResponse, error) {
	if preview == nil || !finitePositiveMeal(preview.ResolvedGrams) {
		return nil, fmt.Errorf("invalid nutrition preview")
	}
	return &nutritionPreviewResponse{
		ResolvedGrams: preview.ResolvedGrams,
		Nutrition: nutritionAmounts{
			CaloriesKcal:   nutrientPointer(preview.Nutrition.Calories),
			ProteinG:       nutrientPointer(preview.Nutrition.Protein),
			CarbohydratesG: nutrientPointer(preview.Nutrition.Carbohydrates),
			FatG:           nutrientPointer(preview.Nutrition.Fat),
		},
	}, nil
}

func mapResolvedFood(resolved *mealai.ResolvedFood) (*resolvedFoodResponse, error) {
	if resolved.FoodID <= 0 {
		return nil, fmt.Errorf("invalid resolved food")
	}
	return &resolvedFoodResponse{
		FoodID: resolved.FoodID, DisplayName: resolved.DisplayName,
		CanonicalName: resolved.CanonicalName, Brand: resolved.Brand,
	}, nil
}

func mapAmountSelection(selection *foodamount.Selection) (*amountSelectionResponse, error) {
	if selection.FoodID <= 0 {
		return nil, fmt.Errorf("invalid selection food")
	}
	response := &amountSelectionResponse{Kind: string(selection.Kind), FoodID: selection.FoodID}
	switch selection.Kind {
	case foodamount.SelectionGrams:
		if selection.Grams == nil || selection.Portion != nil {
			return nil, fmt.Errorf("malformed grams selection")
		}
		response.Grams = &selection.Grams.Grams
	case foodamount.SelectionPortion:
		if selection.Grams != nil || selection.Portion == nil {
			return nil, fmt.Errorf("malformed portion selection")
		}
		response.Portion = &portionSelectionResponse{
			PortionID: selection.Portion.PortionID, Quantity: selection.Portion.Quantity,
			Amount: selection.Portion.Amount, Measure: selection.Portion.Measure,
			PortionGrams: selection.Portion.PortionGrams,
		}
	default:
		return nil, fmt.Errorf("unknown selection kind")
	}
	return response, nil
}

func mapClarification(item mealai.Item) (*mealClarificationResponse, error) {
	if item.Selection != nil || item.Clarification == nil || item.Clarification.Candidates == nil || item.Clarification.Portions == nil {
		return nil, fmt.Errorf("malformed clarification item")
	}
	clarification := item.Clarification
	response := &mealClarificationResponse{
		Kind: string(clarification.Kind), Reason: clarification.Reason,
		Candidates:       make([]foodOptionResponse, 0, len(clarification.Candidates)),
		Portions:         make([]portionResponse, 0, len(clarification.Portions)),
		AllowDirectGrams: clarification.AllowDirectGrams,
	}
	switch clarification.Kind {
	case mealai.ClarificationFoodIdentity:
		if item.Food != nil || len(clarification.Portions) != 0 || clarification.AllowDirectGrams {
			return nil, fmt.Errorf("malformed food clarification")
		}
	case mealai.ClarificationAmount:
		if item.Food == nil || len(clarification.Candidates) != 0 || !clarification.AllowDirectGrams {
			return nil, fmt.Errorf("malformed amount clarification")
		}
	default:
		return nil, fmt.Errorf("unknown clarification kind")
	}
	for _, candidate := range clarification.Candidates {
		response.Candidates = append(response.Candidates, foodOptionResponse{
			FoodID: candidate.FoodID, DisplayName: candidate.DisplayName,
			CanonicalName: candidate.CanonicalName, Brand: candidate.Brand,
		})
	}
	for _, portion := range clarification.Portions {
		response.Portions = append(response.Portions, portionResponse{
			PortionID: portion.ID, Amount: portion.Amount, Measure: portion.Measure, Grams: portion.Grams,
		})
	}
	return response, nil
}

type mealInterpretRequest struct {
	Text   string `json:"text"`
	Locale string `json:"locale"`
}

type mealInterpretResponse struct {
	State string                      `json:"state"`
	Items []mealInterpretItemResponse `json:"items"`
}

type mealInterpretItemResponse struct {
	Mention       string                     `json:"mention"`
	Intent        mealIntentResponse         `json:"intent"`
	State         string                     `json:"state"`
	Food          *resolvedFoodResponse      `json:"food"`
	Selection     *amountSelectionResponse   `json:"selection"`
	Preview       *nutritionPreviewResponse  `json:"preview"`
	Clarification *mealClarificationResponse `json:"clarification"`
}

type mealResolveRequest struct {
	FoodID int64             `json:"food_id"`
	Locale string            `json:"locale"`
	Intent mealIntentRequest `json:"intent"`
	Choice mealChoiceRequest `json:"choice"`
}

type mealIntentRequest struct {
	Query    string   `json:"query"`
	Quantity *float64 `json:"quantity"`
	UnitHint *string  `json:"unit_hint"`
}

type mealChoiceRequest struct {
	Kind      string   `json:"kind"`
	Grams     *float64 `json:"grams"`
	PortionID *int64   `json:"portion_id"`
	Quantity  *float64 `json:"quantity"`
}

type mealResolveResponse struct {
	Intent        mealIntentResponse         `json:"intent"`
	State         string                     `json:"state"`
	Food          *resolvedFoodResponse      `json:"food"`
	Selection     *amountSelectionResponse   `json:"selection"`
	Preview       *nutritionPreviewResponse  `json:"preview"`
	Clarification *mealClarificationResponse `json:"clarification"`
}

type nutritionPreviewResponse struct {
	ResolvedGrams float64          `json:"resolved_grams"`
	Nutrition     nutritionAmounts `json:"nutrition"`
}

type mealIntentResponse struct {
	Query    string   `json:"query"`
	Quantity *float64 `json:"quantity"`
	UnitHint *string  `json:"unit_hint"`
}

type resolvedFoodResponse struct {
	FoodID        int64   `json:"food_id"`
	DisplayName   string  `json:"display_name"`
	CanonicalName string  `json:"canonical_name"`
	Brand         *string `json:"brand"`
}

type foodOptionResponse struct {
	FoodID        int64   `json:"food_id"`
	DisplayName   string  `json:"display_name"`
	CanonicalName string  `json:"canonical_name"`
	Brand         *string `json:"brand"`
}

type amountSelectionResponse struct {
	Kind    string                    `json:"kind"`
	FoodID  int64                     `json:"food_id"`
	Grams   *float64                  `json:"grams"`
	Portion *portionSelectionResponse `json:"portion"`
}

type portionSelectionResponse struct {
	PortionID    int64   `json:"portion_id"`
	Quantity     float64 `json:"quantity"`
	Amount       float64 `json:"amount"`
	Measure      string  `json:"measure"`
	PortionGrams float64 `json:"portion_grams"`
}

type mealClarificationResponse struct {
	Kind             string               `json:"kind"`
	Reason           string               `json:"reason"`
	Candidates       []foodOptionResponse `json:"candidates"`
	Portions         []portionResponse    `json:"portions"`
	AllowDirectGrams bool                 `json:"allow_direct_grams"`
}

func mealIntent(request mealIntentRequest) foodintent.FoodIntent {
	return foodintent.FoodIntent{Query: request.Query, Quantity: request.Quantity, UnitHint: request.UnitHint}
}

func finitePositiveMeal(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}
