BEGIN;

CREATE TABLE food_identifiers (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    food_id BIGINT NOT NULL REFERENCES foods(id) ON DELETE CASCADE,
    scheme TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT food_identifiers_scheme_not_blank CHECK (btrim(scheme) <> ''),
    CONSTRAINT food_identifiers_scheme_supported CHECK (scheme IN ('gtin_upc')),
    CONSTRAINT food_identifiers_value_not_blank CHECK (btrim(value) <> ''),
    CONSTRAINT food_identifiers_scheme_value_unique UNIQUE (scheme, value)
);

CREATE INDEX food_identifiers_food_id_idx ON food_identifiers (food_id);

COMMIT;
