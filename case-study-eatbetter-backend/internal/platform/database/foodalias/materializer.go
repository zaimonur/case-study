// Package foodalias materializes deterministic retrieval-only aliases.
package foodalias

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	TurkishChickenAlias        = "tavuk"
	TurkishLanguageTag         = "tr"
	TurkishChickenMaterializer = "retrieval.tr.chicken.v1"
	maxTurkishChickenAliases   = 8
	catalogAdvisoryLockName    = "eatbetter_usda_food_import"
)

// Candidate is the stable catalog metadata used by retrieval-alias selection.
type Candidate struct {
	FoodID        int64
	CanonicalName string
	IsBranded     bool
}

// Result summarizes one atomic materialization run.
type Result struct {
	SelectedIDs []int64
	Inserted    int64
	Deleted     int64
}

// Materialize opens a transaction and atomically refreshes generated aliases.
func Materialize(ctx context.Context, pool *pgxpool.Pool) (Result, error) {
	if pool == nil {
		return Result{}, fmt.Errorf("database pool is required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin food alias materialization: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	result, err := MaterializeInTransaction(ctx, tx)
	if err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit food alias materialization: %w", err)
	}
	return result, nil
}

// MaterializeInTransaction refreshes only aliases owned by this exact rule.
func MaterializeInTransaction(ctx context.Context, tx pgx.Tx) (Result, error) {
	if tx == nil {
		return Result{}, fmt.Errorf("database transaction is required")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, catalogAdvisoryLockName); err != nil {
		return Result{}, fmt.Errorf("acquire food alias materialization lock: %w", err)
	}

	rows, err := tx.Query(ctx, turkishChickenCandidatesSQL)
	if err != nil {
		return Result{}, fmt.Errorf("query Turkish Chicken alias candidates: %w", err)
	}
	candidates := make([]Candidate, 0, maxTurkishChickenAliases)
	for rows.Next() {
		var candidate Candidate
		if err := rows.Scan(&candidate.FoodID, &candidate.CanonicalName, &candidate.IsBranded); err != nil {
			rows.Close()
			return Result{}, fmt.Errorf("scan Turkish Chicken alias candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("iterate Turkish Chicken alias candidates: %w", err)
	}

	selected := SelectTurkishChickenCandidates(candidates)
	selectedIDs := make([]int64, len(selected))
	for index := range selected {
		selectedIDs[index] = selected[index].FoodID
	}

	deleted, err := tx.Exec(ctx, deleteStaleTurkishChickenAliasesSQL,
		TurkishChickenMaterializer, TurkishChickenAlias, TurkishLanguageTag, selectedIDs)
	if err != nil {
		return Result{}, fmt.Errorf("delete stale Turkish Chicken aliases: %w", err)
	}
	inserted, err := tx.Exec(ctx, insertTurkishChickenAliasesSQL,
		selectedIDs, TurkishChickenAlias, TurkishLanguageTag, TurkishChickenMaterializer)
	if err != nil {
		return Result{}, fmt.Errorf("insert Turkish Chicken aliases: %w", err)
	}

	return Result{
		SelectedIDs: selectedIDs,
		Inserted:    inserted.RowsAffected(),
		Deleted:     deleted.RowsAffected(),
	}, nil
}

// SelectTurkishChickenCandidates chooses broad generic identities in stable semantic order.
// A singleton result is suppressed so the broad alias can never create arbitrary auto-resolution.
func SelectTurkishChickenCandidates(candidates []Candidate) []Candidate {
	selected := make([]Candidate, 0, min(len(turkishChickenPriority), maxTurkishChickenAliases))
	for _, candidate := range candidates {
		if candidate.FoodID <= 0 || candidate.IsBranded {
			continue
		}
		_, eligible := turkishChickenPriority[candidate.CanonicalName]
		if !eligible {
			continue
		}
		selected = append(selected, candidate)
	}

	sort.Slice(selected, func(left, right int) bool {
		leftPriority := turkishChickenPriority[selected[left].CanonicalName]
		rightPriority := turkishChickenPriority[selected[right].CanonicalName]
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return selected[left].FoodID < selected[right].FoodID
	})

	deduplicated := selected[:0]
	seenFoodIDs := make(map[int64]struct{}, len(selected))
	seenNames := make(map[string]struct{}, len(selected))
	for _, candidate := range selected {
		if _, exists := seenFoodIDs[candidate.FoodID]; exists {
			continue
		}
		if _, exists := seenNames[candidate.CanonicalName]; exists {
			continue
		}
		seenFoodIDs[candidate.FoodID] = struct{}{}
		seenNames[candidate.CanonicalName] = struct{}{}
		deduplicated = append(deduplicated, candidate)
		if len(deduplicated) == maxTurkishChickenAliases {
			break
		}
	}
	if len(deduplicated) < 2 {
		return []Candidate{}
	}
	return deduplicated
}

var turkishChickenPriority = map[string]int{
	"Chicken, broilers or fryers, meat only, raw":                 0,
	"Chicken, broilers or fryers, meat only, cooked, roasted":     1,
	"Chicken, broilers or fryers, meat only, cooked, stewed":      2,
	"Chicken, broilers or fryers, meat and skin, raw":             3,
	"Chicken, broilers or fryers, meat and skin, cooked, roasted": 4,
	"Chicken, broilers or fryers, meat and skin, cooked, stewed":  5,
}

const turkishChickenCandidatesSQL = `
SELECT food.id,
       food.canonical_name,
       EXISTS (
           SELECT 1
           FROM food_identifiers AS identifier
           WHERE identifier.food_id = food.id AND identifier.scheme = 'gtin_upc'
       ) AS is_branded
FROM foods AS food
WHERE split_part(food.canonical_name, ',', 1) = 'Chicken'
  AND EXISTS (
      SELECT 1
      FROM external_food_refs AS ref
      WHERE ref.food_id = food.id AND ref.source = 'usda'
  )
ORDER BY food.canonical_name, food.id
`

const deleteStaleTurkishChickenAliasesSQL = `
DELETE FROM food_aliases
WHERE materializer_key = $1
  AND (
      alias <> $2
      OR language_tag IS DISTINCT FROM $3
      OR NOT (food_id = ANY($4::bigint[]))
  )
`

const insertTurkishChickenAliasesSQL = `
INSERT INTO food_aliases (food_id, alias, language_tag, materializer_key)
SELECT food_id, $2, $3, $4
FROM unnest($1::bigint[]) AS selected(food_id)
ON CONFLICT (food_id, alias, language_tag) DO NOTHING
`
