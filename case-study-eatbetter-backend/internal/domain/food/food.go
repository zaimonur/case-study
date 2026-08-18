package food

import (
	"fmt"
	"strings"
)

// Food is the canonical representation of a generic or branded food.
type Food struct {
	ID            int64
	CanonicalName string
	Brand         *string
}

// NewFood creates a canonical food with normalized surrounding whitespace.
func NewFood(canonicalName string, brand *string) (Food, error) {
	canonicalName = strings.TrimSpace(canonicalName)
	if canonicalName == "" {
		return Food{}, fmt.Errorf("canonical name must not be empty")
	}

	var normalizedBrand *string
	if brand != nil {
		value := strings.TrimSpace(*brand)
		if value == "" {
			return Food{}, fmt.Errorf("brand must not be empty when provided")
		}
		normalizedBrand = &value
	}

	return Food{
		CanonicalName: canonicalName,
		Brand:         normalizedBrand,
	}, nil
}
