package foodlocalization

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	t.Cleanup(pool.Close)
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
	t.Cleanup(pool.Close)
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

func TestBrandedFoodSQLScopesIdentifiersToGTINUPC(t *testing.T) {
	t.Parallel()
	if !strings.Contains(brandedFoodSQL, "identifiers.scheme = 'gtin_upc'") {
		t.Fatal("branded food validation must only consider GTIN/UPC identifiers")
	}
}

func TestLoaderReplacesAndRemovesAliasesWithoutReplacingLocalization(t *testing.T) {
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
	t.Cleanup(pool.Close)
	resetLocalizationTestDatabase(t, ctx, pool)

	foodID := insertGenericUSDAFood(t, ctx, pool, "Broccoli, raw", "321900")
	display := "Çiğ brokoli"
	record := localizedTestRecord("321900", "Broccoli, raw", display, []string{"Brokoli", "Taze brokoli"})
	artifact, manifest := writeTestArtifact(t, []app.Record{record})
	loader := Loader{Pool: pool}
	if _, err := loader.Load(ctx, artifact, manifest, false); err != nil {
		t.Fatal(err)
	}
	initial := readLocalizationState(t, ctx, pool, foodID)

	record.Aliases = []string{"Brokoli çiğ"}
	artifact, manifest = writeTestArtifact(t, []app.Record{record})
	if _, err := loader.Load(ctx, artifact, manifest, false); err != nil {
		t.Fatal(err)
	}
	replaced := readLocalizationState(t, ctx, pool, foodID)
	if replaced.ID != initial.ID {
		t.Fatalf("alias replacement changed localization identity: %d -> %d", initial.ID, replaced.ID)
	}
	if replaced.Aliases != "Brokoli çiğ" {
		t.Fatalf("aliases after replacement = %q, want %q", replaced.Aliases, "Brokoli çiğ")
	}

	record.Aliases = []string{}
	artifact, manifest = writeTestArtifact(t, []app.Record{record})
	if _, err := loader.Load(ctx, artifact, manifest, false); err != nil {
		t.Fatal(err)
	}
	removed := readLocalizationState(t, ctx, pool, foodID)
	if removed.ID != initial.ID {
		t.Fatalf("alias removal changed localization identity: %d -> %d", initial.ID, removed.ID)
	}
	if removed.Aliases != "" {
		t.Fatalf("aliases after removal = %q, want none", removed.Aliases)
	}
}

func TestLoaderRejectsMissingUSDAReferenceAndRollsBack(t *testing.T) {
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
	t.Cleanup(pool.Close)
	resetLocalizationTestDatabase(t, ctx, pool)

	foodID := insertGenericUSDAFood(t, ctx, pool, "Broccoli, raw", "321900")
	display := "Çiğ brokoli"
	existing := localizedTestRecord("321900", "Broccoli, raw", display, []string{"Brokoli"})
	artifact, manifest := writeTestArtifact(t, []app.Record{existing})
	loader := Loader{Pool: pool}
	if _, err := loader.Load(ctx, artifact, manifest, false); err != nil {
		t.Fatal(err)
	}
	before := readLocalizationState(t, ctx, pool, foodID)

	missingDisplay := "Çiğ havuç"
	missing := localizedTestRecord("999999", "Carrots, raw", missingDisplay, []string{})
	artifact, manifest = writeTestArtifact(t, []app.Record{missing})
	if _, err := loader.Load(ctx, artifact, manifest, false); err == nil || !strings.Contains(err.Error(), "is not imported") {
		t.Fatalf("missing USDA reference error = %v", err)
	}
	after := readLocalizationState(t, ctx, pool, foodID)
	if after != before {
		t.Fatalf("failed reference validation changed existing localization: before=%+v after=%+v", before, after)
	}
}

func TestLoaderRejectsFingerprintMismatchWithoutMutation(t *testing.T) {
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
	t.Cleanup(pool.Close)
	resetLocalizationTestDatabase(t, ctx, pool)

	foodID := insertGenericUSDAFood(t, ctx, pool, "Broccoli, raw", "321900")
	display := "Çiğ brokoli"
	record := localizedTestRecord("321900", "Broccoli, raw", display, []string{"Brokoli"})
	artifact, manifest := writeTestArtifact(t, []app.Record{record})
	loader := Loader{Pool: pool}
	if _, err := loader.Load(ctx, artifact, manifest, false); err != nil {
		t.Fatal(err)
	}
	before := readLocalizationState(t, ctx, pool, foodID)

	artifact, manifest = writeFingerprintMismatchArtifact(t, record)
	if _, err := loader.Load(ctx, artifact, manifest, false); err == nil || !strings.Contains(err.Error(), "source fingerprint does not match canonical name") {
		t.Fatalf("fingerprint mismatch error = %v", err)
	}
	after := readLocalizationState(t, ctx, pool, foodID)
	if after != before {
		t.Fatalf("failed fingerprint validation changed existing localization: before=%+v after=%+v", before, after)
	}
}

type localizationState struct {
	ID                  int64
	DisplayName         string
	SourceCanonicalName string
	SourceFingerprint   string
	UpdatedAt           time.Time
	Aliases             string
}

func resetLocalizationTestDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `TRUNCATE foods RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset test database (are all migrations applied?): %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `TRUNCATE foods RESTART IDENTITY CASCADE`) })
}

func insertGenericUSDAFood(t *testing.T, ctx context.Context, pool *pgxpool.Pool, canonicalName, externalID string) int64 {
	t.Helper()
	var foodID int64
	if err := pool.QueryRow(ctx, `INSERT INTO foods (canonical_name) VALUES ($1) RETURNING id`, canonicalName).Scan(&foodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO external_food_refs (food_id, source, external_id) VALUES ($1, 'usda', $2)`, foodID, externalID); err != nil {
		t.Fatal(err)
	}
	return foodID
}

func localizedTestRecord(externalID, canonicalName, displayName string, aliases []string) app.Record {
	return app.Record{
		Source: app.SourceUSDA, ExternalID: externalID, DataType: "foundation_food", Locale: app.LocaleTurkish,
		CanonicalName: canonicalName, SourceFingerprint: app.Fingerprint(canonicalName), Status: app.StatusLocalized,
		DisplayName: &displayName, Aliases: aliases, MatchedRuleIDs: []string{"rule"}, ReasonCodes: []string{},
	}
}

func readLocalizationState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, foodID int64) localizationState {
	t.Helper()
	var state localizationState
	if err := pool.QueryRow(ctx, `
SELECT localizations.id, localizations.display_name, localizations.source_canonical_name,
       localizations.source_fingerprint, localizations.updated_at,
       COALESCE(string_agg(aliases.alias, E'\n' ORDER BY aliases.alias), '')
FROM food_localizations AS localizations
LEFT JOIN food_localization_aliases AS aliases ON aliases.localization_id = localizations.id
WHERE localizations.food_id = $1 AND localizations.locale = 'tr'
GROUP BY localizations.id`, foodID).Scan(
		&state.ID, &state.DisplayName, &state.SourceCanonicalName, &state.SourceFingerprint, &state.UpdatedAt, &state.Aliases,
	); err != nil {
		t.Fatal(err)
	}
	return state
}

func writeFingerprintMismatchArtifact(t *testing.T, record app.Record) (string, string) {
	t.Helper()
	artifact, manifestPath := writeTestArtifact(t, []app.Record{record})
	contents, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), record.SourceFingerprint, "sha256:"+strings.Repeat("0", 64), 1))
	if err := os.WriteFile(artifact, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := app.ReadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ArtifactSHA256, err = app.HashFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	return artifact, manifestPath
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
