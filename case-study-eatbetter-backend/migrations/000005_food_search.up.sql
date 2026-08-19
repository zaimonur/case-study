BEGIN;

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Keep Turkish characters in the primary form. Replacing I/İ before lower()
-- makes the result independent of the database collation's Turkish-I behavior.
CREATE FUNCTION food_search_primary(value TEXT)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
PARALLEL SAFE
STRICT
RETURN btrim(
    regexp_replace(
        lower(translate(normalize(value, NFC), 'Iİ', 'ıi')),
        '[[:space:][:punct:]]+',
        ' ',
        'g'
    )
);

CREATE FUNCTION food_search_folded(value TEXT)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
PARALLEL SAFE
STRICT
RETURN translate(food_search_primary(value), 'çğıöşü', 'cgiosu');

CREATE INDEX foods_search_canonical_primary_trgm_idx
    ON foods USING GIN (food_search_primary(canonical_name) gin_trgm_ops);
CREATE INDEX foods_search_canonical_folded_trgm_idx
    ON foods USING GIN (food_search_folded(canonical_name) gin_trgm_ops);
-- Trigram GIN produces many rechecks for short equality probes (for example
-- "milk") on the real catalog. These two measured hot-path indexes are kept
-- separate from prefix/fuzzy GIN support.
CREATE INDEX foods_search_canonical_primary_exact_idx
    ON foods (food_search_primary(canonical_name));
CREATE INDEX foods_search_canonical_folded_exact_idx
    ON foods (food_search_folded(canonical_name));
CREATE INDEX foods_search_brand_primary_trgm_idx
    ON foods USING GIN (food_search_primary(brand) gin_trgm_ops)
    WHERE brand IS NOT NULL;
CREATE INDEX foods_search_brand_folded_trgm_idx
    ON foods USING GIN (food_search_folded(brand) gin_trgm_ops)
    WHERE brand IS NOT NULL;

CREATE INDEX food_aliases_search_primary_trgm_idx
    ON food_aliases USING GIN (food_search_primary(alias) gin_trgm_ops);
CREATE INDEX food_aliases_search_folded_trgm_idx
    ON food_aliases USING GIN (food_search_folded(alias) gin_trgm_ops);

CREATE INDEX food_localizations_search_display_primary_trgm_idx
    ON food_localizations USING GIN (food_search_primary(display_name) gin_trgm_ops);
CREATE INDEX food_localizations_search_display_folded_trgm_idx
    ON food_localizations USING GIN (food_search_folded(display_name) gin_trgm_ops);

CREATE INDEX food_localization_aliases_search_primary_trgm_idx
    ON food_localization_aliases USING GIN (food_search_primary(alias) gin_trgm_ops);
CREATE INDEX food_localization_aliases_search_folded_trgm_idx
    ON food_localization_aliases USING GIN (food_search_folded(alias) gin_trgm_ops);

COMMIT;
