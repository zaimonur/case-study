package mealai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocale"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodresolver"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

var (
	_ TextExtractor       = (*foodextraction.Service)(nil)
	_ FoodResolver        = (*foodresolver.Service)(nil)
	_ AmountResolver      = (*foodamount.Service)(nil)
	_ FoodDetailer        = (*fooddetail.Service)(nil)
	_ NutritionCalculator = (*nutritioncalc.Service)(nil)
)

// Service coordinates extraction, identity resolution, and amount resolution.
type Service struct {
	extractor           TextExtractor
	foodResolver        FoodResolver
	amountResolver      AmountResolver
	foodDetailer        FoodDetailer
	nutritionCalculator NutritionCalculator
}

func NewService(extractor TextExtractor, foodResolver FoodResolver, amountResolver AmountResolver, foodDetailer FoodDetailer, nutritionCalculator NutritionCalculator) *Service {
	return &Service{
		extractor: extractor, foodResolver: foodResolver, amountResolver: amountResolver,
		foodDetailer: foodDetailer, nutritionCalculator: nutritionCalculator,
	}
}

// InterpretText returns a stable initial interpretation without persistence.
func (s *Service) InterpretText(ctx context.Context, request Request) (Result, error) {
	locale, err := foodlocale.Parse(request.Locale)
	if err != nil {
		return Result{}, newError(ErrorInvalidInput, err)
	}

	extraction, err := s.extractor.Extract(ctx, request.Text)
	if err != nil {
		return Result{}, mapExtractionError(err)
	}
	if len(extraction.Items) == 0 {
		return Result{State: StateEmpty, Items: []Item{}}, nil
	}

	items := make([]Item, 0, len(extraction.Items))
	overallState := StateReady
	for _, extracted := range extraction.Items {
		identity, err := s.foodResolver.Resolve(ctx, foodresolver.Request{
			Intent: extracted.Intent, Locale: locale.Exact,
		})
		if err != nil {
			return Result{}, mapFoodResolverError(err)
		}
		if err := validateFoodResolution(identity); err != nil {
			return Result{}, newError(ErrorResolutionFailure, err)
		}

		item := Item{Mention: extracted.Mention, Intent: extracted.Intent}
		switch identity.State {
		case foodresolver.StateAmbiguous, foodresolver.StateNotFound:
			item.State = ItemClarificationRequired
			item.Clarification = foodClarification(identity)
			overallState = StateClarificationRequired
		case foodresolver.StateResolved:
			item.Food = resolvedFood(identity.Resolved)
			amount, err := s.amountResolver.Resolve(ctx, foodamount.Request{
				FoodID: identity.Resolved.FoodID, Intent: extracted.Intent, Locale: locale.Exact,
			})
			if err != nil {
				return Result{}, mapAmountResolverError(err)
			}
			if err := validateAmountResolution(amount, identity.Resolved.FoodID); err != nil {
				return Result{}, newError(ErrorResolutionFailure, err)
			}
			if amount.State == foodamount.StateResolved {
				preview, err := s.calculatePreview(ctx, amount.Selection)
				if err != nil {
					return Result{}, err
				}
				item.State = ItemReady
				item.Selection = amount.Selection
				item.Preview = preview
			} else {
				item.State = ItemClarificationRequired
				item.Clarification = &Clarification{
					Kind: ClarificationAmount, Reason: string(amount.Reason),
					Candidates: []FoodOption{}, Portions: amount.Clarification.Portions,
					AllowDirectGrams: amount.Clarification.AllowDirectGrams,
				}
				overallState = StateClarificationRequired
			}
		}
		items = append(items, item)
	}
	return Result{State: overallState, Items: items}, nil
}

func validateFoodResolution(resolution foodresolver.Resolution) error {
	switch resolution.State {
	case foodresolver.StateResolved:
		if resolution.Resolved == nil || resolution.Resolved.FoodID <= 0 || len(resolution.Candidates) == 0 {
			return fmt.Errorf("malformed resolved food identity")
		}
	case foodresolver.StateAmbiguous:
		if resolution.Resolved != nil || len(resolution.Candidates) == 0 {
			return fmt.Errorf("malformed ambiguous food identity")
		}
	case foodresolver.StateNotFound:
		if resolution.Resolved != nil || resolution.Candidates == nil || len(resolution.Candidates) != 0 {
			return fmt.Errorf("malformed missing food identity")
		}
	default:
		return fmt.Errorf("unknown food identity state")
	}
	return nil
}

func validateAmountResolution(resolution foodamount.Resolution, foodID int64) error {
	switch resolution.State {
	case foodamount.StateResolved:
		if resolution.Selection == nil || resolution.Clarification != nil || resolution.Selection.FoodID != foodID {
			return fmt.Errorf("malformed resolved amount")
		}
		return validateSelection(resolution.Selection)
	case foodamount.StateClarificationRequired:
		if resolution.Selection != nil || resolution.Clarification == nil || resolution.Clarification.Portions == nil || !resolution.Clarification.AllowDirectGrams {
			return fmt.Errorf("malformed amount clarification")
		}
	default:
		return fmt.Errorf("unknown amount state")
	}
	return nil
}

func validateSelection(selection *foodamount.Selection) error {
	if selection == nil {
		return fmt.Errorf("selection is missing")
	}
	if selection.FoodID <= 0 {
		return fmt.Errorf("selection has invalid food identity")
	}
	switch selection.Kind {
	case foodamount.SelectionGrams:
		if selection.Grams == nil || selection.Portion != nil || !finitePositive(selection.Grams.Grams) {
			return fmt.Errorf("malformed grams selection")
		}
	case foodamount.SelectionPortion:
		if selection.Grams != nil || selection.Portion == nil || selection.Portion.PortionID <= 0 ||
			!finitePositive(selection.Portion.Quantity) || !finitePositive(selection.Portion.Amount) ||
			strings.TrimSpace(selection.Portion.Measure) == "" || !finitePositive(selection.Portion.PortionGrams) {
			return fmt.Errorf("malformed portion selection")
		}
	default:
		return fmt.Errorf("unknown selection kind")
	}
	return nil
}

func (s *Service) calculatePreview(ctx context.Context, selection *foodamount.Selection) (*NutritionPreview, error) {
	request, err := nutritionRequest(selection)
	if err != nil {
		return nil, newError(ErrorResolutionFailure, err)
	}
	result, err := s.nutritionCalculator.Calculate(ctx, request)
	if err != nil {
		return nil, mapNutritionError(err)
	}
	if result.FoodID != selection.FoodID || !finitePositive(result.ResolvedGrams) {
		return nil, newError(ErrorResolutionFailure, fmt.Errorf("malformed nutrition result"))
	}
	return &NutritionPreview{ResolvedGrams: result.ResolvedGrams, Nutrition: result.Nutrition}, nil
}

func nutritionRequest(selection *foodamount.Selection) (nutritioncalc.Request, error) {
	if err := validateSelection(selection); err != nil {
		return nutritioncalc.Request{}, err
	}
	request := nutritioncalc.Request{FoodID: selection.FoodID}
	switch selection.Kind {
	case foodamount.SelectionGrams:
		grams := selection.Grams.Grams
		request.Grams = &grams
	case foodamount.SelectionPortion:
		portionID, quantity := selection.Portion.PortionID, selection.Portion.Quantity
		request.PortionID, request.Quantity = &portionID, &quantity
	}
	return request, nil
}

func mapNutritionError(err error) error {
	switch {
	case nutritioncalc.IsValidationError(err):
		return newError(ErrorInvalidInput, err)
	case errors.Is(err, context.Canceled):
		return newError(ErrorCanceled, err)
	case errors.Is(err, context.DeadlineExceeded):
		return newError(ErrorTimeout, err)
	default:
		return newError(ErrorResolutionFailure, err)
	}
}

func finitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

func resolvedFood(candidate *foodsearch.FoodCandidate) *ResolvedFood {
	return &ResolvedFood{
		FoodID: candidate.FoodID, DisplayName: candidate.DisplayName,
		CanonicalName: candidate.CanonicalName, Brand: candidate.Brand,
	}
}

func foodClarification(resolution foodresolver.Resolution) *Clarification {
	options := make([]FoodOption, 0, len(resolution.Candidates))
	for _, candidate := range resolution.Candidates {
		options = append(options, FoodOption{
			FoodID: candidate.FoodID, DisplayName: candidate.DisplayName,
			CanonicalName: candidate.CanonicalName, Brand: candidate.Brand,
		})
	}
	return &Clarification{
		Kind: ClarificationFoodIdentity, Reason: string(resolution.Reason),
		Candidates: options, Portions: []food.Portion{}, AllowDirectGrams: false,
	}
}

func mapExtractionError(err error) error {
	switch {
	case foodextraction.IsKind(err, foodextraction.ErrorInvalidInput):
		return newError(ErrorInvalidInput, err)
	case foodextraction.IsKind(err, foodextraction.ErrorProviderConfiguration):
		return newError(ErrorAIUnavailable, err)
	case foodextraction.IsKind(err, foodextraction.ErrorRateLimit):
		return newError(ErrorAIRateLimited, err)
	case foodextraction.IsKind(err, foodextraction.ErrorTimeout):
		return newError(ErrorAITimeout, err)
	case foodextraction.IsKind(err, foodextraction.ErrorInvalidProviderOutput):
		return newError(ErrorAIInvalidResponse, err)
	case foodextraction.IsKind(err, foodextraction.ErrorProviderFailure):
		return newError(ErrorAIFailure, err)
	case foodextraction.IsKind(err, foodextraction.ErrorCanceled):
		return newError(ErrorCanceled, err)
	default:
		return newError(ErrorAIFailure, err)
	}
}

func mapFoodResolverError(err error) error {
	switch {
	case foodresolver.IsKind(err, foodresolver.ErrorInvalidInput):
		return newError(ErrorInvalidInput, err)
	case foodresolver.IsKind(err, foodresolver.ErrorCanceled):
		return newError(ErrorCanceled, err)
	case foodresolver.IsKind(err, foodresolver.ErrorTimeout):
		return newError(ErrorTimeout, err)
	default:
		return newError(ErrorResolutionFailure, err)
	}
}

func mapAmountResolverError(err error) error {
	switch {
	case foodamount.IsKind(err, foodamount.ErrorInvalidInput):
		return newError(ErrorInvalidInput, err)
	case foodamount.IsKind(err, foodamount.ErrorCanceled):
		return newError(ErrorCanceled, err)
	case foodamount.IsKind(err, foodamount.ErrorTimeout):
		return newError(ErrorTimeout, err)
	default:
		return newError(ErrorResolutionFailure, err)
	}
}
