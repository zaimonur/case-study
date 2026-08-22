package mealai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocale"
)

// ResolveSelection resumes one unresolved item without extraction or food search.
func (s *Service) ResolveSelection(ctx context.Context, request ResolveSelectionRequest) (ResolveSelectionResult, error) {
	if err := validateChoice(request.FoodID, request.Choice); err != nil {
		return ResolveSelectionResult{}, newError(ErrorInvalidInput, err)
	}
	locale, err := foodlocale.Parse(request.Locale)
	if err != nil {
		return ResolveSelectionResult{}, newError(ErrorInvalidInput, err)
	}
	if err := validateContinuationIntent(request.Intent); err != nil {
		return ResolveSelectionResult{}, newError(ErrorInvalidInput, err)
	}

	detail, err := s.foodDetailer.Get(ctx, fooddetail.Request{FoodID: request.FoodID, Locale: locale.Exact})
	if err != nil {
		return ResolveSelectionResult{}, mapFoodDetailError(err)
	}
	if err := validateFoodDetail(detail, request.FoodID); err != nil {
		return ResolveSelectionResult{}, newError(ErrorResolutionFailure, err)
	}

	result := ResolveSelectionResult{Intent: request.Intent, Food: resolvedFoodDetail(detail)}
	var amount foodamount.Resolution
	switch request.Choice.Kind {
	case ChoiceFoodIdentity:
		amount, err = s.amountResolver.Resolve(ctx, foodamount.Request{
			FoodID: request.FoodID, Intent: request.Intent, Locale: locale.Exact,
		})
	case ChoiceGrams:
		resolverIntent := request.Intent
		grams, unit := *request.Choice.Grams, "g"
		resolverIntent.Quantity, resolverIntent.UnitHint = &grams, &unit
		amount, err = s.amountResolver.Resolve(ctx, foodamount.Request{
			FoodID: request.FoodID, Intent: resolverIntent, Locale: locale.Exact,
		})
	case ChoicePortion:
		amount, err = s.amountResolver.ResolvePortionSelection(ctx, foodamount.PortionSelectionRequest{
			FoodID: request.FoodID, Locale: locale.Exact,
			PortionID: *request.Choice.PortionID, Quantity: *request.Choice.Quantity,
		})
	}
	if err != nil {
		return ResolveSelectionResult{}, mapContinuationAmountError(err)
	}
	if err := validateAmountResolution(amount, request.FoodID); err != nil {
		return ResolveSelectionResult{}, newError(ErrorResolutionFailure, err)
	}

	if request.Choice.Kind == ChoiceGrams && (amount.State != foodamount.StateResolved || amount.Selection.Kind != foodamount.SelectionGrams) {
		return ResolveSelectionResult{}, newError(ErrorResolutionFailure, fmt.Errorf("grams choice did not resolve to grams"))
	}
	if request.Choice.Kind == ChoicePortion && (amount.State != foodamount.StateResolved || amount.Selection.Kind != foodamount.SelectionPortion) {
		return ResolveSelectionResult{}, newError(ErrorResolutionFailure, fmt.Errorf("portion choice did not resolve to portion"))
	}

	if amount.State == foodamount.StateClarificationRequired {
		result.State = ItemClarificationRequired
		result.Clarification = amountClarification(amount)
		return result, nil
	}
	preview, err := s.calculatePreview(ctx, amount.Selection)
	if err != nil {
		return ResolveSelectionResult{}, err
	}
	result.State = ItemReady
	result.Selection = amount.Selection
	result.Preview = preview
	return result, nil
}

func validateChoice(foodID int64, choice ExplicitChoice) error {
	if foodID <= 0 {
		return fmt.Errorf("food ID must be positive")
	}
	switch choice.Kind {
	case ChoiceFoodIdentity:
		if choice.Grams != nil || choice.PortionID != nil || choice.Quantity != nil {
			return fmt.Errorf("food identity choice has amount fields")
		}
	case ChoiceGrams:
		if choice.Grams == nil || !finitePositive(*choice.Grams) || choice.PortionID != nil || choice.Quantity != nil {
			return fmt.Errorf("malformed grams choice")
		}
	case ChoicePortion:
		if choice.Grams != nil || choice.PortionID == nil || *choice.PortionID <= 0 || choice.Quantity == nil || !finitePositive(*choice.Quantity) {
			return fmt.Errorf("malformed portion choice")
		}
	default:
		return fmt.Errorf("unknown choice kind")
	}
	return nil
}

func validateContinuationIntent(intent foodintent.FoodIntent) error {
	query := strings.TrimSpace(intent.Query)
	if runes := utf8.RuneCountInString(query); runes < 2 || runes > 120 {
		return fmt.Errorf("invalid intent query")
	}
	if intent.Quantity != nil && !finitePositive(*intent.Quantity) {
		return fmt.Errorf("invalid intent quantity")
	}
	if intent.UnitHint != nil {
		unit := strings.TrimSpace(*intent.UnitHint)
		if unit == "" || utf8.RuneCountInString(unit) > foodextraction.MaxUnitHintRunes {
			return fmt.Errorf("invalid intent unit hint")
		}
	}
	return nil
}

func validateFoodDetail(detail fooddetail.Detail, expectedFoodID int64) error {
	if detail.Food.ID <= 0 || detail.Food.ID != expectedFoodID || strings.TrimSpace(detail.DisplayName) == "" || strings.TrimSpace(detail.Food.CanonicalName) == "" {
		return fmt.Errorf("malformed food detail")
	}
	if detail.Food.Brand != nil && strings.TrimSpace(*detail.Food.Brand) == "" {
		return fmt.Errorf("malformed food brand")
	}
	return nil
}

func resolvedFoodDetail(detail fooddetail.Detail) *ResolvedFood {
	return &ResolvedFood{
		FoodID: detail.Food.ID, DisplayName: detail.DisplayName,
		CanonicalName: detail.Food.CanonicalName, Brand: detail.Food.Brand,
	}
}

func amountClarification(amount foodamount.Resolution) *Clarification {
	return &Clarification{
		Kind: ClarificationAmount, Reason: string(amount.Reason),
		Candidates: []FoodOption{}, Portions: amount.Clarification.Portions,
		AllowDirectGrams: amount.Clarification.AllowDirectGrams,
	}
}

func mapFoodDetailError(err error) error {
	switch {
	case fooddetail.IsValidationError(err):
		return newError(ErrorInvalidInput, err)
	case errors.Is(err, fooddetail.ErrNotFound):
		return newError(ErrorFoodNotFound, err)
	case errors.Is(err, context.Canceled):
		return newError(ErrorCanceled, err)
	case errors.Is(err, context.DeadlineExceeded):
		return newError(ErrorTimeout, err)
	default:
		return newError(ErrorResolutionFailure, err)
	}
}

func mapContinuationAmountError(err error) error {
	switch {
	case foodamount.IsKind(err, foodamount.ErrorInvalidInput):
		return newError(ErrorInvalidInput, err)
	case foodamount.IsKind(err, foodamount.ErrorFoodNotFound):
		return newError(ErrorFoodNotFound, err)
	case foodamount.IsKind(err, foodamount.ErrorPortionNotFound):
		return newError(ErrorPortionNotFound, err)
	case foodamount.IsKind(err, foodamount.ErrorCanceled):
		return newError(ErrorCanceled, err)
	case foodamount.IsKind(err, foodamount.ErrorTimeout):
		return newError(ErrorTimeout, err)
	default:
		return newError(ErrorResolutionFailure, err)
	}
}
