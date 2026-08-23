// Package foodimageextraction validates and orchestrates image food-identity extraction.
package foodimageextraction

import (
	"context"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
)

const (
	MaxImageBytes       = 8 << 20
	MaxItems            = 12
	MaxObservationRunes = 160
	MaxQueryRunes       = 120
)

// ImageInput contains an in-memory image and its declared media type.
type ImageInput struct {
	Data     []byte
	MIMEType string
}

// ExtractedImageFoodIntent keeps visible evidence separate from the reusable intent.
type ExtractedImageFoodIntent struct {
	Observation string
	Intent      foodintent.FoodIntent
}

// ImageFoodExtraction is the image extraction contract at the application boundary.
type ImageFoodExtraction struct {
	Items []ExtractedImageFoodIntent
}

// Extractor is the replaceable image-understanding provider boundary used by Service.
type Extractor interface {
	Extract(context.Context, ImageInput) (ImageFoodExtraction, error)
}
