package food

import (
	"fmt"
	"strings"
)

// FoodAlias is an alternate user-facing name for a canonical food.
type FoodAlias struct {
	ID          int64
	FoodID      int64
	Alias       string
	LanguageTag *string
}

// NewFoodAlias creates an alias with optional language metadata.
func NewFoodAlias(foodID int64, alias string, languageTag *string) (FoodAlias, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return FoodAlias{}, fmt.Errorf("food alias must not be empty")
	}

	var normalizedLanguageTag *string
	if languageTag != nil {
		value := strings.TrimSpace(*languageTag)
		if value == "" {
			return FoodAlias{}, fmt.Errorf("language tag must not be empty when provided")
		}
		normalizedLanguageTag = &value
	}

	return FoodAlias{
		FoodID:      foodID,
		Alias:       alias,
		LanguageTag: normalizedLanguageTag,
	}, nil
}
