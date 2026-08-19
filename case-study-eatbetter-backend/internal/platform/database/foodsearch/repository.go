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
}

// Repository is the focused read-side food-search adapter.
type Repository struct {
	database queryer
}

func New(database queryer) *Repository {
	return &Repository{database: database}
}

// Search executes exact, whole-word, prefix, then (when necessary) fuzzy retrieval.
func (r *Repository) Search(ctx context.Context, query app.Query) ([]app.FoodCandidate, error) {
	cap := query.Limit * stageCapFactor
	if cap < minimumStageCap {
		cap = minimumStageCap
	}

	byFoodID := make(map[int64]app.FoodCandidate, cap)
	if err := r.retrieve(ctx, exactSQL, app.MatchExact, query, cap, byFoodID); err != nil {
		return nil, err
	}
	if len(byFoodID) < query.Limit {
		if err := r.retrieve(ctx, wordSQL, app.MatchWord, query, cap, byFoodID); err != nil {
			return nil, err
		}
	}
	if len(byFoodID) < query.Limit {
		if err := r.retrieve(ctx, prefixSQL, app.MatchPrefix, query, cap, byFoodID); err != nil {
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
	if len(candidates) > query.Limit {
		candidates = candidates[:query.Limit]
	}
	return candidates, nil
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
	if left.Match.Class == app.MatchFuzzy && left.Match.Similarity != right.Match.Similarity {
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
