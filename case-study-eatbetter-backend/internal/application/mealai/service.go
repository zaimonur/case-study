package mealai

import (
	"context"
	"fmt"
	"math"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocale"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodresolver"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

var (
	_ TextExtractor  = (*foodextraction.Service)(nil)
	_ FoodResolver   = (*foodresolver.Service)(nil)
	_ AmountResolver = (*foodamount.Service)(nil)
)

// Service coordinates extraction, identity resolution, and amount resolution.
type Service struct {
	extractor      TextExtractor
	foodResolver   FoodResolver
	amountResolver AmountResolver
}

func NewService(extractor TextExtractor, foodResolver FoodResolver, amountResolver AmountResolver) *Service {
	return &Service{extractor: extractor, foodResolver: foodResolver, amountResolver: amountResolver}
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
				item.State = ItemReady
				item.Selection = amount.Selection
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
	if selection.FoodID <= 0 {
		return fmt.Errorf("selection has invalid food identity")
	}
	switch selection.Kind {
	case foodamount.SelectionGrams:
		if selection.Grams == nil || selection.Portion != nil || !finitePositive(selection.Grams.Grams) {
			return fmt.Errorf("malformed grams selection")
		}
	case foodamount.SelectionPortion:
		if selection.Grams != nil || selection.Portion == nil || selection.Portion.PortionID <= 0 || !finitePositive(selection.Portion.Quantity) {
			return fmt.Errorf("malformed portion selection")
		}
	default:
		return fmt.Errorf("unknown selection kind")
	}
	return nil
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
