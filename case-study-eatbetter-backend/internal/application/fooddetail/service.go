package fooddetail

import (
	"context"
	"errors"
	"fmt"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocale"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

// ValidationError identifies malformed product input.
type ValidationError struct{ Field string }

func (e *ValidationError) Error() string { return fmt.Sprintf("invalid %s", e.Field) }

// IsValidationError reports whether an error is safe to map to invalid_request.
func IsValidationError(err error) bool {
	var validationError *ValidationError
	return errors.As(err, &validationError)
}

// Service validates food detail inputs and delegates persistence.
type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

// Get returns a canonical food detail using a fresh localized display when available.
func (s *Service) Get(ctx context.Context, request Request) (Detail, error) {
	if request.FoodID <= 0 {
		return Detail{}, &ValidationError{Field: "food_id"}
	}
	locale, err := foodlocale.Parse(request.Locale)
	if err != nil {
		return Detail{}, &ValidationError{Field: "locale"}
	}
	detail, err := s.repository.Get(ctx, Query{
		FoodID: request.FoodID, Locale: locale.Exact, BaseLocale: locale.Base,
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Detail{}, ErrNotFound
		}
		return Detail{}, fmt.Errorf("get food detail: %w", err)
	}
	if detail.Portions == nil {
		detail.Portions = []food.Portion{}
	}
	return detail, nil
}
