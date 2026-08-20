package foodsearch

import (
	"context"
	"fmt"

	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
)

func (r *Repository) retrieveProductCandidates(
	ctx context.Context,
	statement string,
	query app.Query,
	cap int,
	arguments []any,
	byFoodID map[int64]app.FoodCandidate,
) error {
	if arguments == nil {
		arguments = []any{query.Primary, query.Folded, query.Locale, query.BaseLocale, cap}
	}
	rows, err := r.database.Query(ctx, statement, arguments...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var candidate app.FoodCandidate
		var class, form, source int16
		if err := rows.Scan(
			&candidate.FoodID, &candidate.CanonicalName, &candidate.DisplayName, &candidate.Brand,
			&candidate.IsBranded, &class, &form, &source, &candidate.Match.Similarity,
		); err != nil {
			return fmt.Errorf("scan product candidate: %w", err)
		}
		candidate.Match.Class = app.MatchClass(class)
		candidate.Match.Form = app.MatchForm(form)
		candidate.Match.Source = app.MatchSource(source)
		current, exists := byFoodID[candidate.FoodID]
		if !exists || stronger(candidate, current) {
			byFoodID[candidate.FoodID] = candidate
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate product candidates: %w", err)
	}
	return nil
}

const genericStrongSQL = `
WITH surfaces AS NOT MATERIALIZED (
    SELECT food.id AS food_id,
           1::smallint AS source_rank,
           food_search_primary(food.canonical_name) AS primary_value,
           food_search_folded(food.canonical_name) AS folded_value
    FROM foods food
    WHERE NOT EXISTS (
        SELECT 1 FROM food_identifiers identifier
        WHERE identifier.food_id = food.id AND identifier.scheme = 'gtin_upc'
    )

    UNION ALL
    SELECT localization.food_id,
           0::smallint,
           food_search_primary(localization.display_name),
           food_search_folded(localization.display_name)
    FROM food_localizations localization
    JOIN foods food ON food.id = localization.food_id
    WHERE localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
      AND NOT EXISTS (
          SELECT 1 FROM food_identifiers identifier
          WHERE identifier.food_id = food.id AND identifier.scheme = 'gtin_upc'
      )

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
      AND NOT EXISTS (
          SELECT 1 FROM food_identifiers identifier
          WHERE identifier.food_id = food.id AND identifier.scheme = 'gtin_upc'
      )

    UNION ALL
    SELECT alias.food_id,
           3::smallint,
           food_search_primary(alias.alias),
           food_search_folded(alias.alias)
    FROM food_aliases alias
    WHERE (alias.language_tag IS NULL OR alias.language_tag IN ($3, $4))
      AND NOT EXISTS (
          SELECT 1 FROM food_identifiers identifier
          WHERE identifier.food_id = alias.food_id AND identifier.scheme = 'gtin_upc'
      )
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
               ELSE 2
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
               ELSE 1
           END::smallint AS form_rank,
           CASE WHEN (
               surface.primary_value LIKE $1 || ' %'
               OR surface.folded_value LIKE $2 || ' %'
           ) THEN 2.0 ELSE 1.0 END::double precision AS similarity
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
       false,
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
