package foodsearch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocale"
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

	parsedLocale, err := foodlocale.Parse(request.Locale)
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

	query := Query{
		Primary: forms.Primary, Folded: forms.Folded,
		Locale: parsedLocale.Exact, BaseLocale: parsedLocale.Base,
		Limit: limit,
	}
	brandMatch, err := s.repository.ResolveBrand(ctx, brandPhrases(forms))
	if err != nil {
		return nil, fmt.Errorf("resolve food brand intent: %w", err)
	}
	if brandMatch != nil {
		remaining := remainingForms(forms, *brandMatch)
		if remaining.Primary != "" {
			candidates, err := s.repository.SearchBranded(ctx, BrandedQuery{
				Query: Query{
					Primary: remaining.Primary, Folded: remaining.Folded,
					Locale: query.Locale, BaseLocale: query.BaseLocale, Limit: query.Limit,
				},
				BrandPrimary: brandMatch.Primary, BrandFolded: brandMatch.Folded,
			})
			return normalizeCandidates(candidates, err, "search branded foods")
		}

		ordinary, err := s.repository.Search(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("search foods: %w", err)
		}
		if hasCredibleGeneric(ordinary) {
			return normalizeCandidates(ordinary, nil, "")
		}
		candidates, err := s.repository.SearchBranded(ctx, BrandedQuery{
			Query: query, BrandPrimary: brandMatch.Primary, BrandFolded: brandMatch.Folded, BrandOnly: true,
		})
		if err != nil {
			return nil, fmt.Errorf("search brand-only foods: %w", err)
		}
		if len(candidates) > 0 {
			return normalizeCandidates(candidates, nil, "")
		}
		return normalizeCandidates(ordinary, nil, "")
	}
	candidates, err := s.repository.Search(ctx, query)
	return normalizeCandidates(candidates, err, "search foods")
}

func normalizeCandidates(candidates []FoodCandidate, err error, operation string) ([]FoodCandidate, error) {
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	if candidates == nil {
		return []FoodCandidate{}, nil
	}
	return candidates, nil
}

func brandPhrases(forms Forms) []BrandPhrase {
	primaryTokens := strings.Fields(forms.Primary)
	foldedTokens := strings.Fields(forms.Folded)
	phrases := make([]BrandPhrase, 0, len(primaryTokens)*(len(primaryTokens)+1)/2)
	// Query length is capped at 120 runes, so the complete contiguous expansion is finite.
	// One repository call resolves all phrases; there is no query-per-combination behavior.
	for size := len(primaryTokens); size >= 1; size-- {
		for start := 0; start+size <= len(primaryTokens); start++ {
			end := start + size
			phrases = append(phrases, BrandPhrase{
				Primary: strings.Join(primaryTokens[start:end], " "),
				Folded:  strings.Join(foldedTokens[start:end], " "),
				Start:   start, End: end, TokenCount: size,
			})
		}
	}
	return phrases
}

func remainingForms(forms Forms, match BrandMatch) Forms {
	primary := strings.Fields(forms.Primary)
	folded := strings.Fields(forms.Folded)
	if match.Start < 0 || match.End > len(primary) || match.Start >= match.End {
		return forms
	}
	primary = append(append([]string{}, primary[:match.Start]...), primary[match.End:]...)
	folded = append(append([]string{}, folded[:match.Start]...), folded[match.End:]...)
	return Forms{Primary: strings.Join(primary, " "), Folded: strings.Join(folded, " ")}
}

func hasCredibleGeneric(candidates []FoodCandidate) bool {
	for _, candidate := range candidates {
		if !candidate.IsBranded && candidate.Match.Source != SourceBrand && candidate.Match.Class <= MatchPrefix {
			return true
		}
	}
	return false
}
