package foodresolver

import (
	"context"
	"errors"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
)

var _ Searcher = (*foodsearch.Service)(nil)

// Service applies identity policy to the existing food-search result.
type Service struct {
	searcher Searcher
}

func NewService(searcher Searcher) *Service { return &Service{searcher: searcher} }

// Resolve performs one search and returns a deterministic identity resolution.
func (s *Service) Resolve(ctx context.Context, request Request) (Resolution, error) {
	candidates, err := s.searcher.Search(ctx, foodsearch.Request{
		Query:    request.Intent.Query,
		Locale:   request.Locale,
		Limit:    foodsearch.DefaultLimit,
		LimitSet: true,
	})
	if err != nil {
		return Resolution{}, classifySearchError(err)
	}
	if len(candidates) == 0 {
		return Resolution{
			State: StateNotFound, Reason: ReasonNoCandidates,
			Candidates: []foodsearch.FoodCandidate{},
		}, nil
	}

	firstExactByFoodID := make(map[int64]int)
	representativeIndex := -1
	for index := range candidates {
		candidate := candidates[index]
		if !isSafeExactIdentity(candidate) {
			continue
		}
		if _, exists := firstExactByFoodID[candidate.FoodID]; !exists {
			firstExactByFoodID[candidate.FoodID] = index
			if representativeIndex < 0 {
				representativeIndex = index
			}
		}
	}

	if len(firstExactByFoodID) > 1 {
		return Resolution{
			State: StateAmbiguous, Reason: ReasonMultipleExactIdentities,
			Candidates: candidates,
		}, nil
	}
	if len(firstExactByFoodID) == 0 {
		return Resolution{
			State: StateAmbiguous, Reason: ReasonNoSafeExactIdentity,
			Candidates: candidates,
		}, nil
	}
	if len(candidates) >= foodsearch.DefaultLimit {
		return Resolution{
			State: StateAmbiguous, Reason: ReasonCandidateSetMayBeTruncated,
			Candidates: candidates,
		}, nil
	}

	return Resolution{
		State: StateResolved, Reason: ReasonUniqueExactIdentity,
		Resolved: &candidates[representativeIndex], Candidates: candidates,
	}, nil
}

func classifySearchError(err error) error {
	switch {
	case foodsearch.IsValidationError(err):
		return newError(ErrorInvalidInput, err)
	case errors.Is(err, context.Canceled):
		return newError(ErrorCanceled, err)
	case errors.Is(err, context.DeadlineExceeded):
		return newError(ErrorTimeout, err)
	default:
		return newError(ErrorSearchFailure, err)
	}
}

func isSafeExactIdentity(candidate foodsearch.FoodCandidate) bool {
	if candidate.Match.Class != foodsearch.MatchExact {
		return false
	}
	switch candidate.Match.Source {
	case foodsearch.SourceLocalizedDisplay,
		foodsearch.SourceCanonicalName,
		foodsearch.SourceLocalizationAlias,
		foodsearch.SourceFoodAlias:
		return true
	default:
		return false
	}
}
