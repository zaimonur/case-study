// Package foodlocalization atomically materializes approved offline localization artifacts.
package foodlocalization

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocalization"
)

const advisoryLockName = "eatbetter_food_localization_load"

// Loader validates and loads a complete localization artifact.
type Loader struct {
	Pool *pgxpool.Pool
}

// Result summarizes the authoritative artifact scope.
type Result struct {
	Eligible       int64
	Localized      int64
	ReviewRequired int64
	Untranslated   int64
	Aliases        int64
	DryRun         bool
}

// Load verifies artifact bytes before starting an atomic PostgreSQL materialization.
func (l Loader) Load(ctx context.Context, artifactPath, manifestPath string, dryRun bool) (Result, error) {
	if l.Pool == nil {
		return Result{}, fmt.Errorf("database pool is required")
	}
	manifest, err := app.ReadManifest(manifestPath)
	if err != nil {
		return Result{}, err
	}
	records := make([]app.Record, 0, manifest.Eligible)
	if err := app.ReadJSONL(artifactPath, manifest, func(record app.Record) error {
		records = append(records, record)
		return nil
	}); err != nil {
		return Result{}, err
	}
	result := Result{
		Eligible: manifest.Eligible, Localized: manifest.Localized,
		ReviewRequired: manifest.ReviewRequired, Untranslated: manifest.Untranslated,
		Aliases: manifest.LocalizedAliases, DryRun: dryRun,
	}

	tx, err := l.Pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin localization load: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, advisoryLockName); err != nil {
		return Result{}, fmt.Errorf("acquire localization load lock: %w", err)
	}
	if _, err := tx.Exec(ctx, temporarySchemaSQL); err != nil {
		return Result{}, fmt.Errorf("create localization staging tables: %w", err)
	}
	if err := stageRecords(ctx, tx, records); err != nil {
		return Result{}, err
	}
	if err := validateStage(ctx, tx); err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx, mergeSQL); err != nil {
		return Result{}, fmt.Errorf("merge food localizations: %w", err)
	}
	if dryRun {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			return Result{}, fmt.Errorf("rollback localization dry run: %w", err)
		}
		return result, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit localization load: %w", err)
	}
	return result, nil
}

func stageRecords(ctx context.Context, tx pgx.Tx, records []app.Record) error {
	rows := make([][]any, len(records))
	aliases := make([][]any, 0)
	for index, record := range records {
		rows[index] = []any{
			record.Source, record.ExternalID, record.DataType, record.Locale, string(record.Status),
			record.CanonicalName, record.SourceFingerprint, record.DisplayName,
		}
		for _, alias := range record.Aliases {
			aliases = append(aliases, []any{record.Source, record.ExternalID, record.Locale, alias})
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"tmp_food_localizations"},
		[]string{"source", "external_id", "data_type", "locale", "status", "canonical_name", "source_fingerprint", "display_name"},
		pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("COPY localization records: %w", err)
	}
	if len(aliases) > 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"tmp_food_localization_aliases"},
			[]string{"source", "external_id", "locale", "alias"}, pgx.CopyFromRows(aliases)); err != nil {
			return fmt.Errorf("COPY localization aliases: %w", err)
		}
	}
	return nil
}

func validateStage(ctx context.Context, tx pgx.Tx) error {
	var externalID string
	if err := tx.QueryRow(ctx, missingReferenceSQL).Scan(&externalID); err == nil {
		return fmt.Errorf("localization USDA FDC ID %q is not imported", externalID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("validate localization references: %w", err)
	}

	var artifactName, databaseName string
	if err := tx.QueryRow(ctx, canonicalMismatchSQL).Scan(&externalID, &artifactName, &databaseName); err == nil {
		return fmt.Errorf("localization USDA FDC ID %q canonical name mismatch: artifact=%q database=%q", externalID, artifactName, databaseName)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("validate localization canonical names: %w", err)
	}

	if err := tx.QueryRow(ctx, brandedFoodSQL).Scan(&externalID); err == nil {
		return fmt.Errorf("localization USDA FDC ID %q resolves to a branded food", externalID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("validate localization generic scope: %w", err)
	}
	return nil
}

const temporarySchemaSQL = `
CREATE TEMP TABLE tmp_food_localizations (
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    data_type TEXT NOT NULL,
    locale TEXT NOT NULL,
    status TEXT NOT NULL,
    canonical_name TEXT NOT NULL,
    source_fingerprint TEXT NOT NULL,
    display_name TEXT,
    PRIMARY KEY (source, external_id)
) ON COMMIT DROP;

CREATE TEMP TABLE tmp_food_localization_aliases (
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    locale TEXT NOT NULL,
    alias TEXT NOT NULL,
    PRIMARY KEY (source, external_id, locale, alias)
) ON COMMIT DROP;
`

const missingReferenceSQL = `
SELECT staged.external_id
FROM tmp_food_localizations AS staged
LEFT JOIN external_food_refs AS refs
  ON refs.source = staged.source AND refs.external_id = staged.external_id
WHERE refs.id IS NULL
ORDER BY staged.external_id
LIMIT 1
`

const canonicalMismatchSQL = `
SELECT staged.external_id, staged.canonical_name, foods.canonical_name
FROM tmp_food_localizations AS staged
JOIN external_food_refs AS refs
  ON refs.source = staged.source AND refs.external_id = staged.external_id
JOIN foods ON foods.id = refs.food_id
WHERE staged.canonical_name IS DISTINCT FROM foods.canonical_name
ORDER BY staged.external_id
LIMIT 1
`

const brandedFoodSQL = `
SELECT staged.external_id
FROM tmp_food_localizations AS staged
JOIN external_food_refs AS refs
  ON refs.source = staged.source AND refs.external_id = staged.external_id
JOIN food_identifiers AS identifiers
  ON identifiers.food_id = refs.food_id
  AND identifiers.scheme = 'gtin_upc'
ORDER BY staged.external_id
LIMIT 1
`

const mergeSQL = `
DELETE FROM food_localizations AS localizations
USING tmp_food_localizations AS staged, external_food_refs AS refs
WHERE refs.source = staged.source
  AND refs.external_id = staged.external_id
  AND localizations.food_id = refs.food_id
  AND localizations.locale = staged.locale
  AND staged.status <> 'localized';

INSERT INTO food_localizations (
    food_id, locale, display_name, source_canonical_name, source_fingerprint
)
SELECT refs.food_id, staged.locale, staged.display_name, staged.canonical_name, staged.source_fingerprint
FROM tmp_food_localizations AS staged
JOIN external_food_refs AS refs
  ON refs.source = staged.source AND refs.external_id = staged.external_id
WHERE staged.status = 'localized'
ON CONFLICT (food_id, locale) DO UPDATE
SET display_name = EXCLUDED.display_name,
    source_canonical_name = EXCLUDED.source_canonical_name,
    source_fingerprint = EXCLUDED.source_fingerprint,
    updated_at = now()
WHERE (
    food_localizations.display_name,
    food_localizations.source_canonical_name,
    food_localizations.source_fingerprint
) IS DISTINCT FROM (
    EXCLUDED.display_name,
    EXCLUDED.source_canonical_name,
    EXCLUDED.source_fingerprint
);

DELETE FROM food_localization_aliases AS aliases
USING food_localizations AS localizations
WHERE aliases.localization_id = localizations.id
  AND EXISTS (
      SELECT 1
      FROM tmp_food_localizations AS staged
      JOIN external_food_refs AS refs
        ON refs.source = staged.source AND refs.external_id = staged.external_id
      WHERE staged.status = 'localized'
        AND refs.food_id = localizations.food_id
        AND staged.locale = localizations.locale
  )
  AND NOT EXISTS (
      SELECT 1
      FROM tmp_food_localization_aliases AS staged_aliases
      JOIN external_food_refs AS refs
        ON refs.source = staged_aliases.source AND refs.external_id = staged_aliases.external_id
      WHERE refs.food_id = localizations.food_id
        AND staged_aliases.locale = localizations.locale
        AND staged_aliases.alias = aliases.alias
  );

INSERT INTO food_localization_aliases (localization_id, alias)
SELECT localizations.id, staged_aliases.alias
FROM tmp_food_localization_aliases AS staged_aliases
JOIN external_food_refs AS refs
  ON refs.source = staged_aliases.source AND refs.external_id = staged_aliases.external_id
JOIN food_localizations AS localizations
  ON localizations.food_id = refs.food_id AND localizations.locale = staged_aliases.locale
ON CONFLICT (localization_id, alias) DO NOTHING;
`
