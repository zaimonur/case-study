package foodlocalization

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocalization"
)

func TestLoaderIsFailClosedIdempotentAndLeavesStaleRowsForResolver(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `TRUNCATE foods RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset test database (are all migrations applied?): %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `TRUNCATE foods RESTART IDENTITY CASCADE`) })

	var foodID int64
	if err := pool.QueryRow(ctx, `INSERT INTO foods (canonical_name) VALUES ('Broccoli, raw') RETURNING id`).Scan(&foodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO external_food_refs (food_id, source, external_id) VALUES ($1, 'usda', '321900')`, foodID); err != nil {
		t.Fatal(err)
	}

	display := "Çiğ brokoli"
	record := app.Record{
		Source: app.SourceUSDA, ExternalID: "321900", DataType: "foundation_food", Locale: app.LocaleTurkish,
		CanonicalName: "Broccoli, raw", SourceFingerprint: app.Fingerprint("Broccoli, raw"),
		Status: app.StatusLocalized, DisplayName: &display, Aliases: []string{"Taze brokoli"},
		MatchedRuleIDs: []string{"rule"}, ReasonCodes: []string{},
	}
	artifact, manifest := writeTestArtifact(t, []app.Record{record})
	loader := Loader{Pool: pool}
	if _, err := loader.Load(ctx, artifact, manifest, false); err != nil {
		t.Fatal(err)
	}
	var localizationID int64
	var firstUpdated time.Time
	if err := pool.QueryRow(ctx, `SELECT id, updated_at FROM food_localizations WHERE food_id=$1 AND locale='tr'`, foodID).Scan(&localizationID, &firstUpdated); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(ctx, artifact, manifest, false); err != nil {
		t.Fatal(err)
	}
	var secondID int64
	var secondUpdated time.Time
	if err := pool.QueryRow(ctx, `SELECT id, updated_at FROM food_localizations WHERE food_id=$1 AND locale='tr'`, foodID).Scan(&secondID, &secondUpdated); err != nil {
		t.Fatal(err)
	}
	if secondID != localizationID || !secondUpdated.Equal(firstUpdated) {
		t.Fatalf("idempotent load changed row: id %d -> %d, updated %v -> %v", localizationID, secondID, firstUpdated, secondUpdated)
	}

	if _, err := pool.Exec(ctx, `UPDATE foods SET canonical_name='Broccoli, cooked' WHERE id=$1`, foodID); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(ctx, artifact, manifest, false); err == nil {
		t.Fatal("stale artifact was accepted")
	}
	var staleCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM food_localizations WHERE id=$1`, localizationID).Scan(&staleCount); err != nil {
		t.Fatal(err)
	}
	if staleCount != 1 {
		t.Fatal("canonical update deleted localization; lifecycle must remain outside canonical import")
	}

	if _, err := pool.Exec(ctx, `UPDATE foods SET canonical_name='Broccoli, raw' WHERE id=$1`, foodID); err != nil {
		t.Fatal(err)
	}
	review := record
	review.Status = app.StatusReviewRequired
	review.DisplayName = nil
	review.Aliases = []string{}
	review.MatchedRuleIDs = []string{}
	review.ReasonCodes = []string{"manual_review"}
	reviewArtifact, reviewManifest := writeTestArtifact(t, []app.Record{review})
	if _, err := loader.Load(ctx, reviewArtifact, reviewManifest, false); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM food_localizations WHERE food_id=$1`, foodID).Scan(&staleCount); err != nil {
		t.Fatal(err)
	}
	if staleCount != 0 {
		t.Fatal("review-required artifact did not remove prior generated localization")
	}
	if _, err := loader.Load(ctx, artifact, manifest, true); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM food_localizations WHERE food_id=$1`, foodID).Scan(&staleCount); err != nil {
		t.Fatal(err)
	}
	if staleCount != 0 {
		t.Fatal("dry run committed localization changes")
	}
}

func TestLoaderRejectsBrandedFood(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE foods RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `TRUNCATE foods RESTART IDENTITY CASCADE`) })
	var foodID int64
	if err := pool.QueryRow(ctx, `INSERT INTO foods (canonical_name, brand) VALUES ('Product', 'Brand') RETURNING id`).Scan(&foodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO external_food_refs (food_id, source, external_id) VALUES ($1, 'usda', '100')`, foodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO food_identifiers (food_id, scheme, value) VALUES ($1, 'gtin_upc', '0001')`, foodID); err != nil {
		t.Fatal(err)
	}
	display := "Ürün"
	record := app.Record{
		Source: app.SourceUSDA, ExternalID: "100", DataType: "foundation_food", Locale: app.LocaleTurkish,
		CanonicalName: "Product", SourceFingerprint: app.Fingerprint("Product"), Status: app.StatusLocalized,
		DisplayName: &display, Aliases: []string{}, MatchedRuleIDs: []string{"rule"}, ReasonCodes: []string{},
	}
	artifact, manifest := writeTestArtifact(t, []app.Record{record})
	if _, err := (Loader{Pool: pool}).Load(ctx, artifact, manifest, false); err == nil {
		t.Fatal("branded food localization was accepted")
	}
}

func writeTestArtifact(t *testing.T, records []app.Record) (string, string) {
	t.Helper()
	directory := t.TempDir()
	artifact := filepath.Join(directory, "tr.jsonl")
	hash, err := app.WriteJSONL(artifact, records)
	if err != nil {
		t.Fatal(err)
	}
	manifestValue := app.NewManifest("2026-04-30", "test-rules", hash, []app.InputFile{
		{Name: "food.csv", SHA256: "0000000000000000000000000000000000000000000000000000000000000000"},
		{Name: "food_nutrient.csv", SHA256: "1111111111111111111111111111111111111111111111111111111111111111"},
		{Name: "nutrient.csv", SHA256: "2222222222222222222222222222222222222222222222222222222222222222"},
	}, records)
	manifest := filepath.Join(directory, "tr.manifest.json")
	if err := app.WriteManifest(manifest, manifestValue); err != nil {
		t.Fatal(err)
	}
	return artifact, manifest
}
