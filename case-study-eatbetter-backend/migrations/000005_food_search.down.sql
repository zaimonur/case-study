BEGIN;

DROP INDEX food_localization_aliases_search_folded_trgm_idx;
DROP INDEX food_localization_aliases_search_primary_trgm_idx;
DROP INDEX food_localizations_search_display_folded_trgm_idx;
DROP INDEX food_localizations_search_display_primary_trgm_idx;
DROP INDEX food_aliases_search_folded_trgm_idx;
DROP INDEX food_aliases_search_primary_trgm_idx;
DROP INDEX foods_search_brand_folded_trgm_idx;
DROP INDEX foods_search_brand_primary_trgm_idx;
DROP INDEX foods_search_canonical_folded_exact_idx;
DROP INDEX foods_search_canonical_primary_exact_idx;
DROP INDEX foods_search_canonical_folded_trgm_idx;
DROP INDEX foods_search_canonical_primary_trgm_idx;

DROP FUNCTION food_search_folded(TEXT);
DROP FUNCTION food_search_primary(TEXT);

-- pg_trgm is intentionally retained: extensions are database-wide shared
-- capabilities and may be used by objects outside this migration's ownership.

COMMIT;
