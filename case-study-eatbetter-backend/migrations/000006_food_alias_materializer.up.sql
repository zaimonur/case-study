BEGIN;

ALTER TABLE food_aliases
    ADD COLUMN materializer_key TEXT,
    ADD CONSTRAINT food_aliases_materializer_key_not_blank
        CHECK (materializer_key IS NULL OR btrim(materializer_key) <> '');

COMMIT;
