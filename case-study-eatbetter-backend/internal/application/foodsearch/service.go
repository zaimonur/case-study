package foodsearch

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"
)

// ValidationError identifies client input that cannot be searched.
type ValidationError struct {
	Field string
}

func (e *ValidationError) Error() string { return fmt.Sprintf("invalid %s", e.Field) }

// IsValidationError reports whether an error represents bad client input.
func IsValidationError(err error) bool {
	var validationError *ValidationError
	return errors.As(err, &validationError)
}

// Service validates and normalizes requests before canonical retrieval.
type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// Search returns candidates; it deliberately does not select a food.
func (s *Service) Search(ctx context.Context, request Request) ([]FoodCandidate, error) {
	forms := Normalize(request.Query)
	queryLength := utf8.RuneCountInString(forms.Primary)
	if queryLength < 2 || queryLength > 120 {
		return nil, &ValidationError{Field: "q"}
	}

	parsedLocale, err := parseLocale(request.Locale)
	if err != nil {
		return nil, &ValidationError{Field: "locale"}
	}

	limit := request.Limit
	if !request.LimitSet {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return nil, &ValidationError{Field: "limit"}
	}

	candidates, err := s.repository.Search(ctx, Query{
		Primary: forms.Primary, Folded: forms.Folded,
		Locale: parsedLocale.exact, BaseLocale: parsedLocale.base,
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search foods: %w", err)
	}
	if candidates == nil {
		candidates = []FoodCandidate{}
	}
	return candidates, nil
}
