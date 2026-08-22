BEGIN;

ALTER TABLE food_aliases
    DROP CONSTRAINT food_aliases_materializer_key_not_blank,
    DROP COLUMN materializer_key;

COMMIT;
