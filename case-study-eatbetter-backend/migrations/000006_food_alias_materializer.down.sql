BEGIN;

DELETE FROM food_aliases
WHERE materializer_key = 'retrieval.tr.chicken.v1';

ALTER TABLE food_aliases
    DROP CONSTRAINT food_aliases_materializer_key_not_blank,
    DROP COLUMN materializer_key;

COMMIT;
