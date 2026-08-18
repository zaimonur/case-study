BEGIN;

CREATE TABLE food_localizations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    food_id BIGINT NOT NULL REFERENCES foods(id) ON DELETE CASCADE,
    locale TEXT NOT NULL,
    display_name TEXT NOT NULL,
    source_canonical_name TEXT NOT NULL,
    source_fingerprint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT food_localizations_locale_not_blank CHECK (btrim(locale) <> ''),
    CONSTRAINT food_localizations_display_name_not_blank CHECK (btrim(display_name) <> ''),
    CONSTRAINT food_localizations_source_canonical_name_not_blank CHECK (btrim(source_canonical_name) <> ''),
    CONSTRAINT food_localizations_source_fingerprint_sha256 CHECK (
        source_fingerprint ~ '^sha256:[0-9a-f]{64}$'
    ),
    CONSTRAINT food_localizations_food_locale_unique UNIQUE (food_id, locale)
);

CREATE TABLE food_localization_aliases (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    localization_id BIGINT NOT NULL REFERENCES food_localizations(id) ON DELETE CASCADE,
    alias TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT food_localization_aliases_alias_not_blank CHECK (btrim(alias) <> ''),
    CONSTRAINT food_localization_aliases_localization_alias_unique UNIQUE (localization_id, alias)
);

COMMIT;
