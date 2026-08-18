BEGIN;

CREATE TABLE foods (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    canonical_name TEXT NOT NULL,
    brand TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT foods_canonical_name_not_blank CHECK (btrim(canonical_name) <> ''),
    CONSTRAINT foods_brand_not_blank CHECK (brand IS NULL OR btrim(brand) <> '')
);

CREATE TABLE food_nutrition (
    food_id BIGINT PRIMARY KEY REFERENCES foods(id) ON DELETE CASCADE,
    calories_per_100g NUMERIC(12, 4),
    protein_per_100g NUMERIC(12, 4),
    carbohydrates_per_100g NUMERIC(12, 4),
    fat_per_100g NUMERIC(12, 4),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT food_nutrition_calories_non_negative CHECK (calories_per_100g IS NULL OR calories_per_100g >= 0),
    CONSTRAINT food_nutrition_protein_non_negative CHECK (protein_per_100g IS NULL OR protein_per_100g >= 0),
    CONSTRAINT food_nutrition_carbohydrates_non_negative CHECK (carbohydrates_per_100g IS NULL OR carbohydrates_per_100g >= 0),
    CONSTRAINT food_nutrition_fat_non_negative CHECK (fat_per_100g IS NULL OR fat_per_100g >= 0),
    CONSTRAINT food_nutrition_has_known_nutrient CHECK (
        calories_per_100g IS NOT NULL
        OR protein_per_100g IS NOT NULL
        OR carbohydrates_per_100g IS NOT NULL
        OR fat_per_100g IS NOT NULL
    )
);

CREATE TABLE food_portions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    food_id BIGINT NOT NULL REFERENCES foods(id) ON DELETE CASCADE,
    amount NUMERIC(12, 4) NOT NULL,
    measure TEXT NOT NULL,
    grams NUMERIC(12, 4) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT food_portions_amount_positive CHECK (amount > 0),
    CONSTRAINT food_portions_measure_not_blank CHECK (btrim(measure) <> ''),
    CONSTRAINT food_portions_grams_positive CHECK (grams > 0),
    CONSTRAINT food_portions_exact_value_unique UNIQUE (food_id, amount, measure, grams)
);

CREATE TABLE food_aliases (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    food_id BIGINT NOT NULL REFERENCES foods(id) ON DELETE CASCADE,
    alias TEXT NOT NULL,
    language_tag TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT food_aliases_alias_not_blank CHECK (btrim(alias) <> ''),
    CONSTRAINT food_aliases_language_tag_not_blank CHECK (language_tag IS NULL OR btrim(language_tag) <> ''),
    CONSTRAINT food_aliases_food_alias_language_unique UNIQUE NULLS NOT DISTINCT (food_id, alias, language_tag)
);

CREATE TABLE external_food_refs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    food_id BIGINT NOT NULL REFERENCES foods(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT external_food_refs_source_not_blank CHECK (btrim(source) <> ''),
    CONSTRAINT external_food_refs_source_supported CHECK (source IN ('usda', 'open_food_facts')),
    CONSTRAINT external_food_refs_external_id_not_blank CHECK (btrim(external_id) <> ''),
    CONSTRAINT external_food_refs_source_external_id_unique UNIQUE (source, external_id)
);

CREATE INDEX food_portions_food_id_idx ON food_portions (food_id);
CREATE INDEX food_aliases_food_id_idx ON food_aliases (food_id);
CREATE INDEX external_food_refs_food_id_idx ON external_food_refs (food_id);

COMMIT;
