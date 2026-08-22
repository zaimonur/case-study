package foodextraction

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Service validates both sides of the external extraction boundary.
type Service struct {
	extractor Extractor
}

func NewService(extractor Extractor) *Service { return &Service{extractor: extractor} }

// Extract validates input, delegates extraction, and validates and safely
// normalizes the provider-independent result.
func (s *Service) Extract(ctx context.Context, text string) (TextFoodExtraction, error) {
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > MaxInputRunes {
		return TextFoodExtraction{}, NewError(ErrorInvalidInput, fmt.Errorf("meal text must contain 1..%d Unicode runes", MaxInputRunes))
	}
	if s == nil || s.extractor == nil {
		return TextFoodExtraction{}, NewError(ErrorProviderConfiguration, fmt.Errorf("text food extractor is not configured"))
	}

	extraction, err := s.extractor.Extract(ctx, text)
	if err != nil {
		var extractionError *Error
		if errors.As(err, &extractionError) {
			return TextFoodExtraction{}, err
		}
		if errors.Is(err, context.Canceled) {
			return TextFoodExtraction{}, NewError(ErrorCanceled, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return TextFoodExtraction{}, NewError(ErrorTimeout, err)
		}
		return TextFoodExtraction{}, NewError(ErrorProviderFailure, err)
	}
	if err := validateAndNormalize(text, &extraction); err != nil {
		return TextFoodExtraction{}, NewError(ErrorInvalidProviderOutput, err)
	}
	if extraction.Items == nil {
		extraction.Items = []ExtractedTextFoodIntent{}
	}
	return extraction, nil
}

func validateAndNormalize(source string, extraction *TextFoodExtraction) error {
	if len(extraction.Items) > MaxItems {
		return fmt.Errorf("items exceeds maximum of %d", MaxItems)
	}

	searchFrom := 0
	for index := range extraction.Items {
		item := &extraction.Items[index]
		if strings.TrimSpace(item.Mention) == "" {
			return fmt.Errorf("items[%d].mention is empty", index)
		}
		relativeStart := strings.Index(source[searchFrom:], item.Mention)
		if relativeStart < 0 {
			return fmt.Errorf("items[%d].mention is not an available source span in order", index)
		}
		searchFrom += relativeStart + len(item.Mention)

		item.Intent.Query = strings.TrimSpace(item.Intent.Query)
		queryRunes := utf8.RuneCountInString(item.Intent.Query)
		if queryRunes < 2 || queryRunes > 120 {
			return fmt.Errorf("items[%d].intent.query must contain 2..120 Unicode runes", index)
		}
		if item.Intent.Quantity != nil && (math.IsNaN(*item.Intent.Quantity) || math.IsInf(*item.Intent.Quantity, 0) || *item.Intent.Quantity <= 0) {
			return fmt.Errorf("items[%d].intent.quantity must be finite and positive", index)
		}
		if item.Intent.UnitHint != nil {
			unitHint := strings.TrimSpace(*item.Intent.UnitHint)
			if unitHint == "" || utf8.RuneCountInString(unitHint) > MaxUnitHintRunes {
				return fmt.Errorf("items[%d].intent.unitHint must contain 1..%d Unicode runes", index, MaxUnitHintRunes)
			}
			unitHint = canonicalUnitHint(unitHint)
			item.Intent.UnitHint = &unitHint
		}
	}
	return nil
}

func canonicalUnitHint(unit string) string {
	switch strings.ToLower(unit) {
	case "gram", "gr", "g":
		return "g"
	case "kilogram", "kg":
		return "kg"
	case "mililitre", "ml":
		return "ml"
	case "litre", "lt", "l":
		return "l"
	case "tane", "adet":
		return "adet"
	default:
		return unit
	}
}
