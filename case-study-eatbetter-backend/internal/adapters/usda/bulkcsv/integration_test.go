package bulkcsv

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	localization "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocalization"
	postgresimport "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/foodimport"
)

func TestPostgresImportIsIdempotentAndPreservesGTINIdentity(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE foods RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset test database (are migrations applied?): %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `TRUNCATE foods RESTART IDENTITY CASCADE`)
	})

	oldDataset := writeSyntheticDatasetVersion(t, false)
	newDataset := writeSyntheticDatasetVersion(t, true)
	importDataset := func(directory string) error {
		_, err := (Importer{
			DatasetDir:  directory,
			DatasetDate: mustDate(t, "2026-04-30"),
			Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
			Stages:      postgresimport.Factory{Pool: pool},
			BatchSize:   2,
		}).Run(context.Background())
		return err
	}

	if err := importDataset(oldDataset); err != nil {
		t.Fatalf("first import: %v", err)
	}
	foodID, name, brand, calories, references := readBrandedState(t, ctx, pool)
	if name != "Old Product" || brand != "Owner Inc" || calories != 100 || references != 1 {
		t.Fatalf("first state = id=%d name=%q brand=%q calories=%v refs=%d", foodID, name, brand, calories, references)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO food_aliases (food_id, alias, language_tag) VALUES ($1, 'untouched alias', 'en')`, foodID); err != nil {
		t.Fatal(err)
	}
	var genericFoodID int64
	if err := pool.QueryRow(ctx, `SELECT food_id FROM external_food_refs WHERE source='usda' AND external_id='200'`).Scan(&genericFoodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO food_localizations (food_id, locale, display_name, source_canonical_name, source_fingerprint)
		VALUES ($1, 'tr', 'Temel yiyecek', 'Foundation Food', $2)
	`, genericFoodID, localization.Fingerprint("Foundation Food")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO external_food_refs (food_id, source, external_id) VALUES ($1, 'open_food_facts', 'off-1')`, foodID); err != nil {
		t.Fatal(err)
	}
	firstCounts := readCanonicalCounts(t, ctx, pool)
	if err := importDataset(oldDataset); err != nil {
		t.Fatalf("same-release re-import: %v", err)
	}
	reimportFoodID, _, _, _, reimportReferences := readBrandedState(t, ctx, pool)
	if reimportFoodID != foodID || reimportReferences != 1 || readCanonicalCounts(t, ctx, pool) != firstCounts {
		t.Fatalf("same-release re-import changed identity/counts: id %d -> %d, refs=%d, counts=%+v -> %+v", foodID, reimportFoodID, reimportReferences, firstCounts, readCanonicalCounts(t, ctx, pool))
	}

	var conflictFoodID int64
	if err := pool.QueryRow(ctx, `INSERT INTO foods (canonical_name) VALUES ('conflicting source record') RETURNING id`).Scan(&conflictFoodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO external_food_refs (food_id, source, external_id) VALUES ($1, 'usda', '101')`, conflictFoodID); err != nil {
		t.Fatal(err)
	}
	countsBeforeFailure := readCanonicalCounts(t, ctx, pool)
	if err := importDataset(newDataset); err == nil {
		t.Fatal("identity-conflicting import succeeded, want atomic failure")
	}
	failedFoodID, failedName, _, failedCalories, failedReferences := readBrandedState(t, ctx, pool)
	if failedFoodID != foodID || failedName != "Old Product" || failedCalories != 100 || failedReferences != 1 || readCanonicalCounts(t, ctx, pool) != countsBeforeFailure {
		t.Fatalf("failed merge was not rolled back: id=%d name=%q calories=%v refs=%d counts=%+v", failedFoodID, failedName, failedCalories, failedReferences, readCanonicalCounts(t, ctx, pool))
	}
	if _, err := pool.Exec(ctx, `DELETE FROM foods WHERE id = $1`, conflictFoodID); err != nil {
		t.Fatal(err)
	}

	if err := importDataset(newDataset); err != nil {
		t.Fatalf("new USDA version import: %v", err)
	}
	updatedFoodID, updatedName, updatedBrand, updatedCalories, updatedReferences := readBrandedState(t, ctx, pool)
	if updatedFoodID != foodID || updatedName != "Current Product" || updatedBrand != "Example Brand" || updatedCalories != 110 || updatedReferences != 2 {
		t.Fatalf("new version state = id=%d name=%q brand=%q calories=%v refs=%d", updatedFoodID, updatedName, updatedBrand, updatedCalories, updatedReferences)
	}
	var portionMeasure string
	var portionGrams float64
	if err := pool.QueryRow(ctx, `SELECT measure, grams FROM food_portions WHERE food_id = $1`, foodID).Scan(&portionMeasure, &portionGrams); err != nil {
		t.Fatal(err)
	}
	if portionMeasure != "2 cookies" || portionGrams != 30 {
		t.Fatalf("replacement portion = %q %v g", portionMeasure, portionGrams)
	}
	var aliases, unrelatedReferences int
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM food_aliases WHERE food_id = $1 AND alias = 'untouched alias'),
			(SELECT count(*) FROM external_food_refs WHERE food_id = $1 AND source = 'open_food_facts' AND external_id = 'off-1')
	`, foodID).Scan(&aliases, &unrelatedReferences); err != nil {
		t.Fatal(err)
	}
	if aliases != 1 || unrelatedReferences != 1 {
		t.Fatalf("unrelated rows changed: aliases=%d references=%d", aliases, unrelatedReferences)
	}
	var localizations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM food_localizations WHERE food_id=$1 AND display_name='Temel yiyecek'`, genericFoodID).Scan(&localizations); err != nil {
		t.Fatal(err)
	}
	if localizations != 1 {
		t.Fatalf("USDA re-import changed localization lifecycle: count=%d", localizations)
	}

	updatedCounts := readCanonicalCounts(t, ctx, pool)
	if err := importDataset(newDataset); err != nil {
		t.Fatalf("new-version re-import: %v", err)
	}
	finalFoodID, _, _, _, finalReferences := readBrandedState(t, ctx, pool)
	if finalFoodID != foodID || finalReferences != 2 || readCanonicalCounts(t, ctx, pool) != updatedCounts {
		t.Fatalf("new-version re-import changed identity/counts: id=%d refs=%d counts=%+v -> %+v", finalFoodID, finalReferences, updatedCounts, readCanonicalCounts(t, ctx, pool))
	}

	var duplicateIdentifiers, duplicateReferences int
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM (SELECT scheme, value FROM food_identifiers GROUP BY scheme, value HAVING count(*) > 1) AS duplicates),
			(SELECT count(*) FROM (SELECT source, external_id FROM external_food_refs GROUP BY source, external_id HAVING count(*) > 1) AS duplicates)
	`).Scan(&duplicateIdentifiers, &duplicateReferences); err != nil {
		t.Fatal(err)
	}
	if duplicateIdentifiers != 0 || duplicateReferences != 0 {
		t.Fatalf("duplicate identities: identifiers=%d references=%d", duplicateIdentifiers, duplicateReferences)
	}
}

type canonicalCounts struct {
	foods       int
	nutrition   int
	portions    int
	identifiers int
	references  int
}

func readCanonicalCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) canonicalCounts {
	t.Helper()
	var counts canonicalCounts
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM foods),
			(SELECT count(*) FROM food_nutrition),
			(SELECT count(*) FROM food_portions),
			(SELECT count(*) FROM food_identifiers),
			(SELECT count(*) FROM external_food_refs)
	`).Scan(&counts.foods, &counts.nutrition, &counts.portions, &counts.identifiers, &counts.references); err != nil {
		t.Fatal(err)
	}
	return counts
}

func readBrandedState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (foodID int64, name, brand string, calories float64, references int) {
	t.Helper()
	if err := pool.QueryRow(ctx, `
		SELECT foods.id, foods.canonical_name, coalesce(foods.brand, ''), nutrition.calories_per_100g,
		       count(refs.id) FILTER (WHERE refs.source = 'usda')
		FROM food_identifiers AS identifiers
		JOIN foods ON foods.id = identifiers.food_id
		JOIN food_nutrition AS nutrition ON nutrition.food_id = foods.id
		LEFT JOIN external_food_refs AS refs ON refs.food_id = foods.id
		WHERE identifiers.scheme = 'gtin_upc' AND identifiers.value = '000000000001'
		GROUP BY foods.id, nutrition.calories_per_100g
	`).Scan(&foodID, &name, &brand, &calories, &references); err != nil {
		t.Fatal(err)
	}
	return foodID, name, brand, calories, references
}
