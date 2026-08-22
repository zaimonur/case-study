package foodamount

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

var _ DetailLoader = (*fooddetail.Service)(nil)

// Service applies deterministic amount policy over trusted application data.
type Service struct {
	detailLoader DetailLoader
}

func NewService(detailLoader DetailLoader) *Service {
	return &Service{detailLoader: detailLoader}
}

// Resolve handles an initial FoodIntent without automatically selecting a stored portion.
func (s *Service) Resolve(ctx context.Context, request Request) (Resolution, error) {
	if request.FoodID <= 0 {
		return Resolution{}, newError(ErrorInvalidInput, fmt.Errorf("food ID must be positive"))
	}
	if request.Intent.Quantity != nil && !isFinitePositive(*request.Intent.Quantity) {
		return Resolution{}, newError(ErrorInvalidInput, fmt.Errorf("quantity must be finite and positive"))
	}
	if request.Intent.Quantity == nil {
		return s.clarification(ctx, request.FoodID, request.Locale, ReasonQuantityRequired)
	}

	if request.Intent.UnitHint == nil || strings.TrimSpace(*request.Intent.UnitHint) == "" {
		return s.clarification(ctx, request.FoodID, request.Locale, ReasonUnitRequired)
	}

	quantity := *request.Intent.Quantity
	switch strings.ToLower(strings.TrimSpace(*request.Intent.UnitHint)) {
	case "g":
		return gramsResolution(request.FoodID, quantity, ReasonExplicitGrams), nil
	case "kg":
		grams := quantity * 1000
		if !isFinitePositive(grams) {
			return Resolution{}, newError(ErrorInvalidInput, fmt.Errorf("kilogram conversion must be finite and positive"))
		}
		return gramsResolution(request.FoodID, grams, ReasonExplicitKilograms), nil
	case "ml", "l":
		return s.clarification(ctx, request.FoodID, request.Locale, ReasonVolumeRequiresClarification)
	default:
		return s.clarification(ctx, request.FoodID, request.Locale, ReasonUnsupportedUnitRequiresClarification)
	}
}

// ResolvePortionSelection validates an explicit user-selected persisted portion.
func (s *Service) ResolvePortionSelection(ctx context.Context, request PortionSelectionRequest) (Resolution, error) {
	if request.FoodID <= 0 || request.PortionID <= 0 || !isFinitePositive(request.Quantity) {
		return Resolution{}, newError(ErrorInvalidInput, fmt.Errorf("food ID, portion ID, and quantity must be positive"))
	}

	detail, err := s.loadDetail(ctx, request.FoodID, request.Locale)
	if err != nil {
		return Resolution{}, err
	}
	for _, portion := range detail.Portions {
		if portion.ID != request.PortionID || portion.FoodID != request.FoodID {
			continue
		}
		return Resolution{
			State: StateResolved, Reason: ReasonExplicitPortionSelection,
			Selection: &Selection{
				Kind: SelectionPortion, FoodID: request.FoodID,
				Portion: &PortionSelection{
					PortionID: portion.ID, Quantity: request.Quantity,
					Amount: portion.Amount, Measure: portion.Measure, PortionGrams: portion.Grams,
				},
			},
		}, nil
	}
	return Resolution{}, newError(ErrorPortionNotFound, fmt.Errorf("selected portion does not belong to food"))
}

func (s *Service) clarification(ctx context.Context, foodID int64, locale string, reason Reason) (Resolution, error) {
	detail, err := s.loadDetail(ctx, foodID, locale)
	if err != nil {
		return Resolution{}, err
	}
	portions := detail.Portions
	if portions == nil {
		portions = []food.Portion{}
	}
	return Resolution{
		State: StateClarificationRequired, Reason: reason,
		Clarification: &Clarification{Portions: portions, AllowDirectGrams: true},
	}, nil
}

func (s *Service) loadDetail(ctx context.Context, foodID int64, locale string) (fooddetail.Detail, error) {
	detail, err := s.detailLoader.Get(ctx, fooddetail.Request{FoodID: foodID, Locale: locale})
	if err != nil {
		return fooddetail.Detail{}, classifyDetailError(err)
	}
	return detail, nil
}

func classifyDetailError(err error) error {
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
		return newError(ErrorDetailFailure, err)
	}
}

func gramsResolution(foodID int64, grams float64, reason Reason) Resolution {
	return Resolution{
		State: StateResolved, Reason: reason,
		Selection: &Selection{
			Kind: SelectionGrams, FoodID: foodID,
			Grams: &GramsSelection{Grams: grams},
		},
	}
}

func isFinitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}
