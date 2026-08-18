package food

import (
	"fmt"
	"strings"
)

// FoodSource identifies the external system that owns a source-specific food record.
type FoodSource string

const (
	FoodSourceUSDA          FoodSource = "usda"
	FoodSourceOpenFoodFacts FoodSource = "open_food_facts"
)

// ExternalFoodReference links a canonical food to a source-owned string identifier.
type ExternalFoodReference struct {
	ID         int64
	FoodID     int64
	Source     FoodSource
	ExternalID string
}

// NewExternalFoodReference creates a reference to a supported external food source.
func NewExternalFoodReference(foodID int64, source FoodSource, externalID string) (ExternalFoodReference, error) {
	if source != FoodSourceUSDA && source != FoodSourceOpenFoodFacts {
		return ExternalFoodReference{}, fmt.Errorf("unsupported food source %q", source)
	}

	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return ExternalFoodReference{}, fmt.Errorf("external food ID must not be empty")
	}

	return ExternalFoodReference{
		FoodID:     foodID,
		Source:     source,
		ExternalID: externalID,
	}, nil
}
