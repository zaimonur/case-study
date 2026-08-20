package foodsearch

import (
	"context"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
)

// ResolveBrand proves all contiguous query phrases in one bounded, parameterized query.
func (r *Repository) ResolveBrand(ctx context.Context, phrases []app.BrandPhrase) (*app.BrandMatch, error) {
	if len(phrases) == 0 {
		return nil, nil
	}
	primary := make([]string, len(phrases))
	folded := make([]string, len(phrases))
	starts := make([]int32, len(phrases))
	ends := make([]int32, len(phrases))
	counts := make([]int32, len(phrases))
	for index, phrase := range phrases {
		primary[index], folded[index] = phrase.Primary, phrase.Folded
		starts[index], ends[index], counts[index] = int32(phrase.Start), int32(phrase.End), int32(phrase.TokenCount)
	}
	var match app.BrandMatch
	err := r.database.QueryRow(ctx, resolveBrandSQL, primary, folded, starts, ends, counts).Scan(
		&match.Primary, &match.Folded, &match.Start, &match.End,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve persisted brand phrase: %w", err)
	}
	return &match, nil
}

// SearchBranded searches a product phrase inside the resolved persisted brand,
// or returns a deterministic bounded brand catalog for brand-only intent.
func (r *Repository) SearchBranded(ctx context.Context, query app.BrandedQuery) ([]app.FoodCandidate, error) {
	cap := query.Limit * stageCapFactor
	if cap < minimumStageCap {
		cap = minimumStageCap
	}
	statement := brandProductSQL
	arguments := []any{
		query.Primary, query.Folded, query.Locale, query.BaseLocale, cap,
		query.BrandPrimary, query.BrandFolded, utf8.RuneCountInString(query.Primary) >= 3,
	}
	if query.BrandOnly {
		statement = brandOnlySQL
		arguments = []any{query.BrandPrimary, query.BrandFolded, query.Locale, query.BaseLocale, cap}
	}
	rows, err := r.database.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("retrieve branded candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]app.FoodCandidate, 0, cap)
	for rows.Next() {
		var candidate app.FoodCandidate
		var class, form, source int16
		if err := rows.Scan(
			&candidate.FoodID, &candidate.CanonicalName, &candidate.DisplayName, &candidate.Brand,
			&candidate.IsBranded, &class, &form, &source, &candidate.Match.Similarity,
		); err != nil {
			return nil, fmt.Errorf("scan branded candidate: %w", err)
		}
		candidate.Match.Class = app.MatchClass(class)
		candidate.Match.Form = app.MatchForm(form)
		candidate.Match.Source = app.MatchSource(source)
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate branded candidates: %w", err)
	}
	sort.Slice(candidates, func(left, right int) bool { return stronger(candidates[left], candidates[right]) })
	if len(candidates) > query.Limit {
		candidates = candidates[:query.Limit]
	}
	return candidates, nil
}

const resolveBrandSQL = `
WITH phrases AS (
    SELECT *
    FROM unnest($1::text[], $2::text[], $3::integer[], $4::integer[], $5::integer[])
         AS phrase(primary_value, folded_value, start_index, end_index, token_count)
),
matches AS (
    SELECT phrase.primary_value,
           phrase.folded_value,
           phrase.start_index,
           phrase.end_index,
           phrase.token_count,
           bool_or(food_search_primary(food.brand) = phrase.primary_value) AS primary_match
    FROM phrases phrase
    JOIN foods food ON food.brand IS NOT NULL
      AND (
          food_search_primary(food.brand) = phrase.primary_value
          OR food_search_folded(food.brand) = phrase.folded_value
      )
    GROUP BY phrase.primary_value,
             phrase.folded_value,
             phrase.start_index,
             phrase.end_index,
             phrase.token_count
)
SELECT match.primary_value, match.folded_value, match.start_index, match.end_index
FROM matches match
ORDER BY match.token_count DESC,
         char_length(match.primary_value) DESC,
         CASE WHEN match.primary_match THEN 0 ELSE 1 END,
         match.start_index,
         match.primary_value
LIMIT 1`

const brandProductSQL = `
WITH surfaces AS (
    SELECT food.id AS food_id,
           1::smallint AS source_rank,
           food_search_primary(food.canonical_name) AS primary_value,
           food_search_folded(food.canonical_name) AS folded_value
    FROM foods food
    WHERE food.brand IS NOT NULL
      AND (food_search_primary(food.brand) = $6 OR food_search_folded(food.brand) = $7)

    UNION ALL
    SELECT localization.food_id,
           0::smallint,
           food_search_primary(localization.display_name),
           food_search_folded(localization.display_name)
    FROM food_localizations localization
    JOIN foods food ON food.id = localization.food_id
    WHERE localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
      AND food.brand IS NOT NULL
      AND (food_search_primary(food.brand) = $6 OR food_search_folded(food.brand) = $7)

    UNION ALL
    SELECT localization.food_id,
           2::smallint,
           food_search_primary(alias.alias),
           food_search_folded(alias.alias)
    FROM food_localization_aliases alias
    JOIN food_localizations localization ON localization.id = alias.localization_id
    JOIN foods food ON food.id = localization.food_id
    WHERE localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
      AND food.brand IS NOT NULL
      AND (food_search_primary(food.brand) = $6 OR food_search_folded(food.brand) = $7)

    UNION ALL
    SELECT alias.food_id,
           3::smallint,
           food_search_primary(alias.alias),
           food_search_folded(alias.alias)
    FROM food_aliases alias
    JOIN foods food ON food.id = alias.food_id
    WHERE (alias.language_tag IS NULL OR alias.language_tag IN ($3, $4))
      AND food.brand IS NOT NULL
      AND (food_search_primary(food.brand) = $6 OR food_search_folded(food.brand) = $7)
),
matched AS (
    SELECT surface.food_id,
           surface.source_rank,
           CASE
               WHEN surface.primary_value = $1 OR surface.folded_value = $2 THEN 0
               WHEN surface.primary_value LIKE $1 || ' %'
                 OR surface.primary_value LIKE '% ' || $1
                 OR surface.primary_value LIKE '% ' || $1 || ' %'
                 OR surface.folded_value LIKE $2 || ' %'
                 OR surface.folded_value LIKE '% ' || $2
                 OR surface.folded_value LIKE '% ' || $2 || ' %' THEN 1
               WHEN surface.primary_value LIKE $1 || '%'
                 OR surface.folded_value LIKE $2 || '%' THEN 2
               ELSE 3
           END::smallint AS class_rank,
           CASE
               WHEN surface.primary_value = $1 THEN 0
               WHEN surface.folded_value = $2 THEN 1
               WHEN surface.primary_value LIKE $1 || ' %'
                 OR surface.primary_value LIKE '% ' || $1
                 OR surface.primary_value LIKE '% ' || $1 || ' %' THEN 0
               WHEN surface.folded_value LIKE $2 || ' %'
                 OR surface.folded_value LIKE '% ' || $2
                 OR surface.folded_value LIKE '% ' || $2 || ' %' THEN 1
               WHEN surface.primary_value LIKE $1 || '%' THEN 0
               WHEN surface.folded_value LIKE $2 || '%' THEN 1
               WHEN surface.primary_value %> $1 THEN 0
               ELSE 1
           END::smallint AS form_rank,
           CASE
               WHEN $8 AND (surface.primary_value %> $1 OR surface.folded_value %> $2)
               THEN GREATEST(
                   word_similarity($1, surface.primary_value),
                   word_similarity($2, surface.folded_value)
               )::double precision
               ELSE 1.0::double precision
           END AS similarity
    FROM surfaces surface
    WHERE surface.primary_value = $1 OR surface.folded_value = $2
       OR surface.primary_value LIKE $1 || ' %'
       OR surface.primary_value LIKE '% ' || $1
       OR surface.primary_value LIKE '% ' || $1 || ' %'
       OR surface.folded_value LIKE $2 || ' %'
       OR surface.folded_value LIKE '% ' || $2
       OR surface.folded_value LIKE '% ' || $2 || ' %'
       OR surface.primary_value LIKE $1 || '%'
       OR surface.folded_value LIKE $2 || '%'
       OR ($8 AND (surface.primary_value %> $1 OR surface.folded_value %> $2))
),
best AS (
    SELECT DISTINCT ON (food_id) food_id, class_rank, form_rank, source_rank, similarity
    FROM matched
    ORDER BY food_id, class_rank, form_rank, source_rank, similarity DESC
),
bounded AS (
    SELECT food_id, class_rank, form_rank, source_rank, similarity
    FROM best
    ORDER BY class_rank, form_rank, source_rank, similarity DESC, food_id
    LIMIT $5
)
SELECT food.id,
       food.canonical_name,
       COALESCE(display.display_name, food.canonical_name),
       food.brand,
       EXISTS (
           SELECT 1 FROM food_identifiers identifier
           WHERE identifier.food_id = food.id AND identifier.scheme = 'gtin_upc'
       ),
       bounded.class_rank,
       bounded.form_rank,
       bounded.source_rank,
       bounded.similarity
FROM bounded
JOIN foods food ON food.id = bounded.food_id
LEFT JOIN LATERAL (
    SELECT localization.display_name
    FROM food_localizations localization
    WHERE localization.food_id = food.id
      AND localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
    ORDER BY CASE WHEN localization.locale = $3 THEN 0 ELSE 1 END, localization.id
    LIMIT 1
) display ON TRUE
ORDER BY bounded.class_rank, bounded.form_rank, bounded.source_rank, bounded.similarity DESC, food.id`

const brandOnlySQL = `
WITH bounded AS (
    SELECT food.id,
           CASE WHEN food_search_primary(food.brand) = $1 THEN 0 ELSE 1 END::smallint AS form_rank
    FROM foods food
    WHERE food.brand IS NOT NULL
      AND (food_search_primary(food.brand) = $1 OR food_search_folded(food.brand) = $2)
    ORDER BY form_rank, food.id
    LIMIT $5
)
SELECT food.id,
       food.canonical_name,
       COALESCE(display.display_name, food.canonical_name),
       food.brand,
       EXISTS (
           SELECT 1 FROM food_identifiers identifier
           WHERE identifier.food_id = food.id AND identifier.scheme = 'gtin_upc'
       ),
       0::smallint,
       bounded.form_rank,
       4::smallint,
       1.0::double precision
FROM bounded
JOIN foods food ON food.id = bounded.id
LEFT JOIN LATERAL (
    SELECT localization.display_name
    FROM food_localizations localization
    WHERE localization.food_id = food.id
      AND localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
    ORDER BY CASE WHEN localization.locale = $3 THEN 0 ELSE 1 END, localization.id
    LIMIT 1
) display ON TRUE
ORDER BY bounded.form_rank, food.id`
