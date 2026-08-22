// Package foodextraction validates and orchestrates text food-intent extraction.
package foodextraction

import (
	"context"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
)

const (
	MaxInputRunes    = 2000
	MaxItems         = 12
	MaxUnitHintRunes = 32
)

// ExtractedTextFoodIntent keeps source evidence separate from the reusable intent.
type ExtractedTextFoodIntent struct {
	Mention string
	Intent  foodintent.FoodIntent
}

// TextFoodExtraction is extraction contract v1 at the application boundary.
type TextFoodExtraction struct {
	Items []ExtractedTextFoodIntent
}

// Extractor is the replaceable provider boundary used by Service.
type Extractor interface {
	Extract(context.Context, string) (TextFoodExtraction, error)
}
