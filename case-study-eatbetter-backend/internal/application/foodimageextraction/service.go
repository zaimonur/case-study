package foodimageextraction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Service validates both sides of the external image extraction boundary.
type Service struct {
	extractor Extractor
}

func NewService(extractor Extractor) *Service { return &Service{extractor: extractor} }

// Extract validates input before delegation, then validates and normalizes the
// provider-independent result. It never derives amounts from image evidence.
func (s *Service) Extract(ctx context.Context, input ImageInput) (ImageFoodExtraction, error) {
	normalizedInput, err := validateInput(input)
	if err != nil {
		return ImageFoodExtraction{}, NewError(ErrorInvalidInput, err)
	}
	if s == nil || s.extractor == nil {
		return ImageFoodExtraction{}, NewError(ErrorProviderConfiguration, fmt.Errorf("image food extractor is not configured"))
	}

	extraction, err := s.extractor.Extract(ctx, normalizedInput)
	if err != nil {
		var extractionError *Error
		if errors.As(err, &extractionError) {
			return ImageFoodExtraction{}, err
		}
		if errors.Is(err, context.Canceled) {
			return ImageFoodExtraction{}, NewError(ErrorCanceled, context.Canceled)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return ImageFoodExtraction{}, NewError(ErrorTimeout, context.DeadlineExceeded)
		}
		return ImageFoodExtraction{}, NewError(ErrorProviderFailure, fmt.Errorf("image food extractor failed"))
	}
	if err := validateAndNormalize(&extraction); err != nil {
		return ImageFoodExtraction{}, NewError(ErrorInvalidProviderOutput, err)
	}
	if extraction.Items == nil {
		extraction.Items = []ExtractedImageFoodIntent{}
	}
	return extraction, nil
}

func validateInput(input ImageInput) (ImageInput, error) {
	if len(input.Data) == 0 {
		return ImageInput{}, fmt.Errorf("image data is required")
	}
	if len(input.Data) > MaxImageBytes {
		return ImageInput{}, fmt.Errorf("image data exceeds %d bytes", MaxImageBytes)
	}
	mimeType := strings.ToLower(strings.TrimSpace(input.MIMEType))
	switch mimeType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return ImageInput{}, fmt.Errorf("image MIME type is unsupported")
	}
	input.MIMEType = mimeType
	return input, nil
}

func validateAndNormalize(extraction *ImageFoodExtraction) error {
	if len(extraction.Items) > MaxItems {
		return fmt.Errorf("items exceeds maximum of %d", MaxItems)
	}
	for index := range extraction.Items {
		item := &extraction.Items[index]
		item.Observation = strings.TrimSpace(item.Observation)
		observationRunes := utf8.RuneCountInString(item.Observation)
		if observationRunes < 1 || observationRunes > MaxObservationRunes {
			return fmt.Errorf("items[%d].observation must contain 1..%d Unicode runes", index, MaxObservationRunes)
		}
		item.Intent.Query = strings.TrimSpace(item.Intent.Query)
		queryRunes := utf8.RuneCountInString(item.Intent.Query)
		if queryRunes < 2 || queryRunes > MaxQueryRunes {
			return fmt.Errorf("items[%d].intent.query must contain 2..%d Unicode runes", index, MaxQueryRunes)
		}
		if item.Intent.Quantity != nil {
			return fmt.Errorf("items[%d].intent.quantity must be nil", index)
		}
		if item.Intent.UnitHint != nil {
			return fmt.Errorf("items[%d].intent.unitHint must be nil", index)
		}
	}
	return nil
}
