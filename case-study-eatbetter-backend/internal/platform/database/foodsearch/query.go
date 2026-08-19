package foodsearch

// Every statement has the same parameters:
// $1 primary query, $2 folded query, $3 exact locale, $4 base locale, $5 stage cap.
// User input is never interpolated into SQL.

const exactSQL = `
WITH signals AS (
    SELECT localization.food_id,
           CASE WHEN food_search_primary(localization.display_name) = $1 THEN 0 ELSE 1 END::smallint AS form_rank,
           0::smallint AS source_rank,
           1.0::double precision AS similarity
    FROM food_localizations localization
    JOIN foods food ON food.id = localization.food_id
    WHERE localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
      AND (food_search_primary(localization.display_name) = $1 OR food_search_folded(localization.display_name) = $2)

    UNION ALL
    SELECT food.id,
           CASE WHEN food_search_primary(food.canonical_name) = $1 THEN 0 ELSE 1 END::smallint,
           1::smallint,
           1.0::double precision
    FROM foods food
    WHERE food_search_primary(food.canonical_name) = $1 OR food_search_folded(food.canonical_name) = $2

    UNION ALL
    SELECT localization.food_id,
           CASE WHEN food_search_primary(alias.alias) = $1 THEN 0 ELSE 1 END::smallint,
           2::smallint,
           1.0::double precision
    FROM food_localization_aliases alias
    JOIN food_localizations localization ON localization.id = alias.localization_id
    JOIN foods food ON food.id = localization.food_id
    WHERE localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
      AND (food_search_primary(alias.alias) = $1 OR food_search_folded(alias.alias) = $2)

    UNION ALL
    SELECT alias.food_id,
           CASE WHEN food_search_primary(alias.alias) = $1 THEN 0 ELSE 1 END::smallint,
           3::smallint,
           1.0::double precision
    FROM food_aliases alias
    WHERE (alias.language_tag IS NULL OR alias.language_tag IN ($3, $4))
      AND (food_search_primary(alias.alias) = $1 OR food_search_folded(alias.alias) = $2)

    UNION ALL
    SELECT food.id,
           CASE WHEN food_search_primary(food.brand) = $1 THEN 0 ELSE 1 END::smallint,
           4::smallint,
           1.0::double precision
    FROM foods food
    WHERE food.brand IS NOT NULL
      AND (food_search_primary(food.brand) = $1 OR food_search_folded(food.brand) = $2)
),
best AS (
    SELECT DISTINCT ON (food_id) food_id, form_rank, source_rank, similarity
    FROM signals
    ORDER BY food_id, form_rank, source_rank, similarity DESC
),
bounded AS (
    SELECT food_id, form_rank, source_rank, similarity
    FROM best
    ORDER BY form_rank, source_rank, food_id
    LIMIT $5
)
SELECT food.id,
       food.canonical_name,
       COALESCE(display.display_name, food.canonical_name),
       food.brand,
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
ORDER BY bounded.form_rank, bounded.source_rank, food.id`

const prefixSQL = `
WITH signals AS (
    SELECT localization.food_id,
           CASE WHEN food_search_primary(localization.display_name) LIKE $1 || '%' THEN 0 ELSE 1 END::smallint AS form_rank,
           0::smallint AS source_rank,
           1.0::double precision AS similarity
    FROM food_localizations localization
    JOIN foods food ON food.id = localization.food_id
    WHERE localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
      AND (food_search_primary(localization.display_name) LIKE $1 || '%' OR food_search_folded(localization.display_name) LIKE $2 || '%')

    UNION ALL
    SELECT food.id,
           CASE WHEN food_search_primary(food.canonical_name) LIKE $1 || '%' THEN 0 ELSE 1 END::smallint,
           1::smallint,
           1.0::double precision
    FROM foods food
    WHERE food_search_primary(food.canonical_name) LIKE $1 || '%' OR food_search_folded(food.canonical_name) LIKE $2 || '%'

    UNION ALL
    SELECT localization.food_id,
           CASE WHEN food_search_primary(alias.alias) LIKE $1 || '%' THEN 0 ELSE 1 END::smallint,
           2::smallint,
           1.0::double precision
    FROM food_localization_aliases alias
    JOIN food_localizations localization ON localization.id = alias.localization_id
    JOIN foods food ON food.id = localization.food_id
    WHERE localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
      AND (food_search_primary(alias.alias) LIKE $1 || '%' OR food_search_folded(alias.alias) LIKE $2 || '%')

    UNION ALL
    SELECT alias.food_id,
           CASE WHEN food_search_primary(alias.alias) LIKE $1 || '%' THEN 0 ELSE 1 END::smallint,
           3::smallint,
           1.0::double precision
    FROM food_aliases alias
    WHERE (alias.language_tag IS NULL OR alias.language_tag IN ($3, $4))
      AND (food_search_primary(alias.alias) LIKE $1 || '%' OR food_search_folded(alias.alias) LIKE $2 || '%')

    UNION ALL
    SELECT food.id,
           CASE WHEN food_search_primary(food.brand) LIKE $1 || '%' THEN 0 ELSE 1 END::smallint,
           4::smallint,
           1.0::double precision
    FROM foods food
    WHERE food.brand IS NOT NULL
      AND (food_search_primary(food.brand) LIKE $1 || '%' OR food_search_folded(food.brand) LIKE $2 || '%')
),
best AS (
    SELECT DISTINCT ON (food_id) food_id, form_rank, source_rank, similarity
    FROM signals
    ORDER BY food_id, form_rank, source_rank, similarity DESC
),
bounded AS (
    SELECT food_id, form_rank, source_rank, similarity
    FROM best
    ORDER BY form_rank, source_rank, food_id
    LIMIT $5
)
SELECT food.id,
       food.canonical_name,
       COALESCE(display.display_name, food.canonical_name),
       food.brand,
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
ORDER BY bounded.form_rank, bounded.source_rank, food.id`

const fuzzySQL = `
WITH signals AS (
    SELECT localization.food_id,
           CASE WHEN food_search_primary(localization.display_name) %> $1 THEN 0 ELSE 1 END::smallint AS form_rank,
           0::smallint AS source_rank,
           GREATEST(
               word_similarity($1, food_search_primary(localization.display_name)),
               word_similarity($2, food_search_folded(localization.display_name))
           )::double precision AS similarity
    FROM food_localizations localization
    JOIN foods food ON food.id = localization.food_id
    WHERE localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
      AND (food_search_primary(localization.display_name) %> $1 OR food_search_folded(localization.display_name) %> $2)

    UNION ALL
    SELECT food.id,
           CASE WHEN food_search_primary(food.canonical_name) %> $1 THEN 0 ELSE 1 END::smallint,
           1::smallint,
           GREATEST(
               word_similarity($1, food_search_primary(food.canonical_name)),
               word_similarity($2, food_search_folded(food.canonical_name))
           )::double precision
    FROM foods food
    WHERE food_search_primary(food.canonical_name) %> $1 OR food_search_folded(food.canonical_name) %> $2

    UNION ALL
    SELECT localization.food_id,
           CASE WHEN food_search_primary(alias.alias) %> $1 THEN 0 ELSE 1 END::smallint,
           2::smallint,
           GREATEST(
               word_similarity($1, food_search_primary(alias.alias)),
               word_similarity($2, food_search_folded(alias.alias))
           )::double precision
    FROM food_localization_aliases alias
    JOIN food_localizations localization ON localization.id = alias.localization_id
    JOIN foods food ON food.id = localization.food_id
    WHERE localization.source_canonical_name = food.canonical_name
      AND localization.locale IN ($3, $4)
      AND (food_search_primary(alias.alias) %> $1 OR food_search_folded(alias.alias) %> $2)

    UNION ALL
    SELECT alias.food_id,
           CASE WHEN food_search_primary(alias.alias) %> $1 THEN 0 ELSE 1 END::smallint,
           3::smallint,
           GREATEST(
               word_similarity($1, food_search_primary(alias.alias)),
               word_similarity($2, food_search_folded(alias.alias))
           )::double precision
    FROM food_aliases alias
    WHERE (alias.language_tag IS NULL OR alias.language_tag IN ($3, $4))
      AND (food_search_primary(alias.alias) %> $1 OR food_search_folded(alias.alias) %> $2)

    UNION ALL
    SELECT food.id,
           CASE WHEN food_search_primary(food.brand) %> $1 THEN 0 ELSE 1 END::smallint,
           4::smallint,
           GREATEST(
               word_similarity($1, food_search_primary(food.brand)),
               word_similarity($2, food_search_folded(food.brand))
           )::double precision
    FROM foods food
    WHERE food.brand IS NOT NULL
      AND (food_search_primary(food.brand) %> $1 OR food_search_folded(food.brand) %> $2)
),
best AS (
    SELECT DISTINCT ON (food_id) food_id, form_rank, source_rank, similarity
    FROM signals
    ORDER BY food_id, form_rank, source_rank, similarity DESC
),
bounded AS (
    SELECT food_id, form_rank, source_rank, similarity
    FROM best
    ORDER BY form_rank, source_rank, similarity DESC, food_id
    LIMIT $5
)
SELECT food.id,
       food.canonical_name,
       COALESCE(display.display_name, food.canonical_name),
       food.brand,
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
ORDER BY bounded.form_rank, bounded.source_rank, bounded.similarity DESC, food.id`
