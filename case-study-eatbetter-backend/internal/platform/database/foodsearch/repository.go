// Package foodsearch implements bounded PostgreSQL canonical-food retrieval.
package foodsearch

import (
	"context"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
)

const (
	minimumStageCap = 40
	stageCapFactor  = 5
)

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Repository is the focused read-side food-search adapter.
type Repository struct {
	database queryer
}

func New(database queryer) *Repository {
	return &Repository{database: database}
}

// Search collects every strong lexical stage before product-aware composition.
func (r *Repository) Search(ctx context.Context, query app.Query) ([]app.FoodCandidate, error) {
	cap := query.Limit * stageCapFactor
	if cap < minimumStageCap {
		cap = minimumStageCap
	}

	byFoodID := make(map[int64]app.FoodCandidate, cap)
	if err := r.retrieve(ctx, exactSQL, app.MatchExact, query, cap, byFoodID); err != nil {
		return nil, err
	}
	genericChecked, err := r.retrieveGenericIfCrowded(ctx, query, cap, byFoodID, false)
	if err != nil {
		return nil, err
	}
	if len(byFoodID) < query.Limit || !hasCredibleGeneric(byFoodID) {
		if err := r.retrieve(ctx, wordSQL, app.MatchWord, query, cap, byFoodID); err != nil {
			return nil, err
		}
		genericChecked, err = r.retrieveGenericIfCrowded(ctx, query, cap, byFoodID, genericChecked)
		if err != nil {
			return nil, err
		}
	}
	if len(byFoodID) < query.Limit || !hasCredibleGeneric(byFoodID) {
		if err := r.retrieve(ctx, prefixSQL, app.MatchPrefix, query, cap, byFoodID); err != nil {
			return nil, err
		}
		_, err = r.retrieveGenericIfCrowded(ctx, query, cap, byFoodID, genericChecked)
		if err != nil {
			return nil, err
		}
	}
	if len(byFoodID) < query.Limit && utf8.RuneCountInString(query.Primary) >= 3 {
		if err := r.retrieve(ctx, fuzzySQL, app.MatchFuzzy, query, cap, byFoodID); err != nil {
			return nil, err
		}
	}

	candidates := make([]app.FoodCandidate, 0, len(byFoodID))
	for _, candidate := range byFoodID {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(left, right int) bool { return stronger(candidates[left], candidates[right]) })
	return composeOrdinary(candidates, query.Limit), nil
}

func (r *Repository) retrieveGenericIfCrowded(
	ctx context.Context,
	query app.Query,
	cap int,
	byFoodID map[int64]app.FoodCandidate,
	alreadyChecked bool,
) (bool, error) {
	if alreadyChecked || len(byFoodID) < query.Limit || hasCredibleGeneric(byFoodID) {
		return alreadyChecked, nil
	}
	if err := r.retrieveProductCandidates(ctx, genericStrongSQL, query, cap, nil, byFoodID); err != nil {
		return true, fmt.Errorf("retrieve generic product candidates: %w", err)
	}
	return true, nil
}

func hasCredibleGeneric(candidates map[int64]app.FoodCandidate) bool {
	for _, candidate := range candidates {
		if !candidate.IsBranded && candidate.Match.Source != app.SourceBrand && candidate.Match.Class <= app.MatchPrefix {
			return true
		}
	}
	return false
}

func (r *Repository) retrieve(
	ctx context.Context,
	statement string,
	class app.MatchClass,
	query app.Query,
	cap int,
	byFoodID map[int64]app.FoodCandidate,
) error {
	rows, err := r.database.Query(ctx, statement, query.Primary, query.Folded, query.Locale, query.BaseLocale, cap)
	if err != nil {
		return fmt.Errorf("retrieve %s candidates: %w", matchClassName(class), err)
	}
	defer rows.Close()

	for rows.Next() {
		var candidate app.FoodCandidate
		var form, source int16
		if err := rows.Scan(
			&candidate.FoodID,
			&candidate.CanonicalName,
			&candidate.DisplayName,
			&candidate.Brand,
			&candidate.IsBranded,
			&form,
			&source,
			&candidate.Match.Similarity,
		); err != nil {
			return fmt.Errorf("scan %s candidate: %w", matchClassName(class), err)
		}
		candidate.Match.Class = class
		candidate.Match.Form = app.MatchForm(form)
		candidate.Match.Source = app.MatchSource(source)

		current, exists := byFoodID[candidate.FoodID]
		if !exists || stronger(candidate, current) {
			byFoodID[candidate.FoodID] = candidate
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s candidates: %w", matchClassName(class), err)
	}
	return nil
}

// composeOrdinary reserves at most half the public response for lexically credible
// generic/common foods, then fills remaining slots in ordinary lexical order.
// Fuzzy generic candidates receive no product-priority lane.
func composeOrdinary(candidates []app.FoodCandidate, limit int) []app.FoodCandidate {
	result := make([]app.FoodCandidate, 0, min(limit, len(candidates)))
	selected := make(map[int64]struct{}, limit)
	genericBudget := (limit + 1) / 2
	for _, candidate := range candidates {
		if len(result) >= genericBudget {
			break
		}
		if candidate.IsBranded || candidate.Match.Source == app.SourceBrand || candidate.Match.Class > app.MatchPrefix {
			continue
		}
		result = append(result, candidate)
		selected[candidate.FoodID] = struct{}{}
	}
	for _, candidate := range candidates {
		if len(result) >= limit {
			break
		}
		if _, exists := selected[candidate.FoodID]; exists {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

func stronger(left, right app.FoodCandidate) bool {
	if left.Match.Class != right.Match.Class {
		return left.Match.Class < right.Match.Class
	}
	if left.Match.Form != right.Match.Form {
		return left.Match.Form < right.Match.Form
	}
	if left.Match.Source != right.Match.Source {
		return left.Match.Source < right.Match.Source
	}
	if (left.Match.Class == app.MatchWord || left.Match.Class == app.MatchFuzzy) && left.Match.Similarity != right.Match.Similarity {
		return left.Match.Similarity > right.Match.Similarity
	}
	return left.FoodID < right.FoodID
}

func matchClassName(class app.MatchClass) string {
	switch class {
	case app.MatchExact:
		return "exact"
	case app.MatchWord:
		return "word"
	case app.MatchPrefix:
		return "prefix"
	case app.MatchFuzzy:
		return "fuzzy"
	default:
		return "unknown"
	}
}
