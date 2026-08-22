// Package foodimport persists provider-neutral import records with PostgreSQL COPY and an atomic merge.
package foodimport

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	appimport "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodimport"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/foodalias"
)

const advisoryLockName = "eatbetter_usda_food_import"

// Factory creates import stages on dedicated pooled connections.
type Factory struct {
	Pool *pgxpool.Pool
}

// Begin creates canonical-shaped temporary tables inside an isolated transaction.
func (f Factory) Begin(ctx context.Context) (appimport.Stage, error) {
	if f.Pool == nil {
		return nil, fmt.Errorf("database pool is required")
	}
	connection, err := f.Pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire import connection: %w", err)
	}
	tx, err := connection.Begin(ctx)
	if err != nil {
		connection.Release()
		return nil, fmt.Errorf("begin import transaction: %w", err)
	}
	stage := &postgresStage{connection: connection, tx: tx}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, advisoryLockName); err != nil {
		_ = stage.Rollback(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("acquire USDA import advisory lock: %w", err)
	}
	if _, err := tx.Exec(ctx, temporarySchemaSQL); err != nil {
		_ = stage.Rollback(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("create USDA import temporary tables: %w", err)
	}
	return stage, nil
}

type postgresStage struct {
	connection *pgxpool.Conn
	tx         pgx.Tx
	closed     bool
}

func (s *postgresStage) StageFoods(ctx context.Context, foods []appimport.Food) error {
	if len(foods) == 0 {
		return nil
	}
	rows := make([][]any, len(foods))
	for index, food := range foods {
		rows[index] = []any{
			food.ImportKey,
			nullableString(food.GTIN),
			food.SelectedFDCID,
			food.CanonicalName,
			nullableString(food.Brand),
			nullableFloat(food.Nutrition.Calories),
			nullableFloat(food.Nutrition.Protein),
			nullableFloat(food.Nutrition.Carbohydrates),
			nullableFloat(food.Nutrition.Fat),
		}
	}
	_, err := s.tx.CopyFrom(ctx,
		pgx.Identifier{"tmp_usda_foods"},
		[]string{"import_key", "gtin", "selected_fdc_id", "canonical_name", "brand", "calories", "protein", "carbohydrates", "fat"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("COPY staged USDA foods: %w", err)
	}
	return nil
}

func (s *postgresStage) StageReferences(ctx context.Context, references []appimport.Reference) error {
	if len(references) == 0 {
		return nil
	}
	rows := make([][]any, len(references))
	for index, reference := range references {
		rows[index] = []any{reference.ImportKey, reference.ExternalID}
	}
	_, err := s.tx.CopyFrom(ctx,
		pgx.Identifier{"tmp_usda_refs"},
		[]string{"import_key", "fdc_id"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("COPY staged USDA references: %w", err)
	}
	return nil
}

func (s *postgresStage) StagePortions(ctx context.Context, portions []appimport.Portion) error {
	if len(portions) == 0 {
		return nil
	}
	rows := make([][]any, len(portions))
	for index, portion := range portions {
		rows[index] = []any{portion.ImportKey, portion.Amount, portion.Measure, portion.Grams}
	}
	_, err := s.tx.CopyFrom(ctx,
		pgx.Identifier{"tmp_usda_portions"},
		[]string{"import_key", "amount", "measure", "grams"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("COPY staged USDA portions: %w", err)
	}
	return nil
}

func (s *postgresStage) Commit(ctx context.Context) (appimport.MergeResult, error) {
	if s.closed {
		return appimport.MergeResult{}, fmt.Errorf("import stage is already closed")
	}
	defer s.release()

	if _, err := s.tx.Exec(ctx, temporaryIndexesSQL); err != nil {
		_ = s.tx.Rollback(context.WithoutCancel(ctx))
		return appimport.MergeResult{}, fmt.Errorf("index USDA import staging tables: %w", err)
	}
	if err := s.validateExistingIdentities(ctx); err != nil {
		_ = s.tx.Rollback(context.WithoutCancel(ctx))
		return appimport.MergeResult{}, err
	}
	if _, err := s.tx.Exec(ctx, resolveAndMergeSQL); err != nil {
		_ = s.tx.Rollback(context.WithoutCancel(ctx))
		return appimport.MergeResult{}, fmt.Errorf("merge staged USDA foods: %w", err)
	}
	if err := s.validateResolvedReferences(ctx); err != nil {
		_ = s.tx.Rollback(context.WithoutCancel(ctx))
		return appimport.MergeResult{}, err
	}
	if _, err := s.tx.Exec(ctx, relatedRowsMergeSQL); err != nil {
		_ = s.tx.Rollback(context.WithoutCancel(ctx))
		return appimport.MergeResult{}, fmt.Errorf("merge staged USDA related rows: %w", err)
	}
	if _, err := foodalias.MaterializeInTransaction(ctx, s.tx); err != nil {
		_ = s.tx.Rollback(context.WithoutCancel(ctx))
		return appimport.MergeResult{}, fmt.Errorf("materialize retrieval aliases: %w", err)
	}

	var result appimport.MergeResult
	if err := s.tx.QueryRow(ctx, stagingCountsSQL).Scan(
		&result.Foods,
		&result.Identifiers,
		&result.References,
		&result.Nutrition,
		&result.Portions,
	); err != nil {
		_ = s.tx.Rollback(context.WithoutCancel(ctx))
		return appimport.MergeResult{}, fmt.Errorf("read USDA merge counts: %w", err)
	}
	if err := s.tx.Commit(ctx); err != nil {
		return appimport.MergeResult{}, fmt.Errorf("commit USDA import transaction: %w", err)
	}
	return result, nil
}

func (s *postgresStage) Rollback(ctx context.Context) error {
	if s.closed {
		return nil
	}
	defer s.release()
	err := s.tx.Rollback(ctx)
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}
	return err
}

func (s *postgresStage) validateExistingIdentities(ctx context.Context) error {
	var importKey string
	var foodIDs []int64
	err := s.tx.QueryRow(ctx, existingReferenceGroupConflictSQL).Scan(&importKey, &foodIDs)
	if err == nil {
		return fmt.Errorf("USDA import identity %q has historical references attached to multiple foods %v", importKey, foodIDs)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("validate historical USDA reference groups: %w", err)
	}

	var gtinFoodID, referenceFoodID int64
	err = s.tx.QueryRow(ctx, identifierReferenceConflictSQL).Scan(&importKey, &gtinFoodID, &referenceFoodID)
	if err == nil {
		return fmt.Errorf("USDA import identity %q maps GTIN to food %d but a historical FDC reference to food %d", importKey, gtinFoodID, referenceFoodID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("validate GTIN and USDA reference agreement: %w", err)
	}
	return nil
}

func (s *postgresStage) validateResolvedReferences(ctx context.Context) error {
	var fdcID string
	var existingFoodID, selectedFoodID int64
	err := s.tx.QueryRow(ctx, resolvedReferenceConflictSQL).Scan(&fdcID, &existingFoodID, &selectedFoodID)
	if err == nil {
		return fmt.Errorf("USDA FDC ID %q is attached to food %d, cannot attach it to food %d", fdcID, existingFoodID, selectedFoodID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return fmt.Errorf("validate resolved USDA references: %w", err)
}

func (s *postgresStage) release() {
	s.closed = true
	s.connection.Release()
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

const temporarySchemaSQL = `
CREATE TEMP TABLE tmp_usda_foods (
    import_key TEXT PRIMARY KEY,
    gtin TEXT,
    selected_fdc_id TEXT NOT NULL,
    canonical_name TEXT NOT NULL,
    brand TEXT,
    calories NUMERIC,
    protein NUMERIC,
    carbohydrates NUMERIC,
    fat NUMERIC,
    food_id BIGINT
) ON COMMIT DROP;

CREATE TEMP TABLE tmp_usda_refs (
    import_key TEXT NOT NULL,
    fdc_id TEXT NOT NULL
) ON COMMIT DROP;

CREATE TEMP TABLE tmp_usda_portions (
    import_key TEXT NOT NULL,
    amount NUMERIC NOT NULL,
    measure TEXT NOT NULL,
    grams NUMERIC NOT NULL
) ON COMMIT DROP;
`

const temporaryIndexesSQL = `
CREATE INDEX tmp_usda_refs_import_key_idx ON tmp_usda_refs (import_key);
CREATE INDEX tmp_usda_refs_fdc_id_idx ON tmp_usda_refs (fdc_id);
CREATE INDEX tmp_usda_portions_import_key_idx ON tmp_usda_portions (import_key);
ANALYZE tmp_usda_foods;
ANALYZE tmp_usda_refs;
ANALYZE tmp_usda_portions;
`

const existingReferenceGroupConflictSQL = `
SELECT staged.import_key, array_agg(DISTINCT refs.food_id ORDER BY refs.food_id)
FROM (SELECT DISTINCT import_key, fdc_id FROM tmp_usda_refs) AS staged
JOIN external_food_refs AS refs
  ON refs.source = 'usda' AND refs.external_id = staged.fdc_id
JOIN tmp_usda_foods AS foods ON foods.import_key = staged.import_key
GROUP BY staged.import_key
HAVING count(DISTINCT refs.food_id) > 1
LIMIT 1
`

const identifierReferenceConflictSQL = `
SELECT foods.import_key, identifiers.food_id, refs.food_id
FROM tmp_usda_foods AS foods
JOIN food_identifiers AS identifiers
  ON foods.gtin IS NOT NULL
 AND identifiers.scheme = 'gtin_upc'
 AND identifiers.value = foods.gtin
JOIN tmp_usda_refs AS staged_refs ON staged_refs.import_key = foods.import_key
JOIN external_food_refs AS refs
  ON refs.source = 'usda' AND refs.external_id = staged_refs.fdc_id
WHERE identifiers.food_id <> refs.food_id
LIMIT 1
`

const resolveAndMergeSQL = `
UPDATE tmp_usda_foods AS staged
SET food_id = identifiers.food_id
FROM food_identifiers AS identifiers
WHERE staged.gtin IS NOT NULL
  AND identifiers.scheme = 'gtin_upc'
  AND identifiers.value = staged.gtin;

UPDATE tmp_usda_foods AS staged
SET food_id = matched.food_id
FROM (
    SELECT refs_stage.import_key, min(refs.food_id) AS food_id
    FROM (SELECT DISTINCT import_key, fdc_id FROM tmp_usda_refs) AS refs_stage
    JOIN external_food_refs AS refs
      ON refs.source = 'usda' AND refs.external_id = refs_stage.fdc_id
    GROUP BY refs_stage.import_key
) AS matched
WHERE staged.import_key = matched.import_key
  AND staged.food_id IS NULL;

UPDATE tmp_usda_foods
SET food_id = nextval(pg_get_serial_sequence('foods', 'id'))
WHERE food_id IS NULL;

INSERT INTO foods (id, canonical_name, brand)
OVERRIDING SYSTEM VALUE
SELECT staged.food_id, staged.canonical_name, staged.brand
FROM tmp_usda_foods AS staged
WHERE NOT EXISTS (SELECT 1 FROM foods WHERE foods.id = staged.food_id);

UPDATE foods
SET canonical_name = staged.canonical_name,
    brand = staged.brand,
    updated_at = now()
FROM tmp_usda_foods AS staged
WHERE foods.id = staged.food_id
  AND (foods.canonical_name, foods.brand) IS DISTINCT FROM (staged.canonical_name, staged.brand);

INSERT INTO food_identifiers (food_id, scheme, value)
SELECT staged.food_id, 'gtin_upc', staged.gtin
FROM tmp_usda_foods AS staged
WHERE staged.gtin IS NOT NULL
ON CONFLICT (scheme, value) DO NOTHING;
`

const resolvedReferenceConflictSQL = `
SELECT staged_refs.fdc_id, refs.food_id, foods.food_id
FROM (SELECT DISTINCT import_key, fdc_id FROM tmp_usda_refs) AS staged_refs
JOIN tmp_usda_foods AS foods ON foods.import_key = staged_refs.import_key
JOIN external_food_refs AS refs
  ON refs.source = 'usda' AND refs.external_id = staged_refs.fdc_id
WHERE refs.food_id <> foods.food_id
LIMIT 1
`

const relatedRowsMergeSQL = `
INSERT INTO external_food_refs (food_id, source, external_id)
SELECT DISTINCT foods.food_id, 'usda', staged_refs.fdc_id
FROM tmp_usda_refs AS staged_refs
JOIN tmp_usda_foods AS foods ON foods.import_key = staged_refs.import_key
ON CONFLICT (source, external_id) DO NOTHING;

INSERT INTO food_nutrition (
    food_id,
    calories_per_100g,
    protein_per_100g,
    carbohydrates_per_100g,
    fat_per_100g
)
SELECT food_id, calories, protein, carbohydrates, fat
FROM tmp_usda_foods
ON CONFLICT (food_id) DO UPDATE
SET calories_per_100g = EXCLUDED.calories_per_100g,
    protein_per_100g = EXCLUDED.protein_per_100g,
    carbohydrates_per_100g = EXCLUDED.carbohydrates_per_100g,
    fat_per_100g = EXCLUDED.fat_per_100g,
    updated_at = now()
WHERE (
    food_nutrition.calories_per_100g,
    food_nutrition.protein_per_100g,
    food_nutrition.carbohydrates_per_100g,
    food_nutrition.fat_per_100g
) IS DISTINCT FROM (
    EXCLUDED.calories_per_100g,
    EXCLUDED.protein_per_100g,
    EXCLUDED.carbohydrates_per_100g,
    EXCLUDED.fat_per_100g
);

DELETE FROM food_portions AS portions
USING tmp_usda_foods AS foods
WHERE portions.food_id = foods.food_id;

INSERT INTO food_portions (food_id, amount, measure, grams)
SELECT DISTINCT foods.food_id, portions.amount, portions.measure, portions.grams
FROM tmp_usda_portions AS portions
JOIN tmp_usda_foods AS foods ON foods.import_key = portions.import_key;
`

const stagingCountsSQL = `
SELECT
    (SELECT count(*) FROM tmp_usda_foods),
    (SELECT count(*) FROM tmp_usda_foods WHERE gtin IS NOT NULL),
    (SELECT count(*) FROM (SELECT DISTINCT staged_refs.fdc_id FROM tmp_usda_refs AS staged_refs JOIN tmp_usda_foods AS foods ON foods.import_key = staged_refs.import_key) AS refs),
    (SELECT count(*) FROM tmp_usda_foods),
    (SELECT count(*) FROM (SELECT DISTINCT import_key, amount, measure, grams FROM tmp_usda_portions) AS portions)
`
