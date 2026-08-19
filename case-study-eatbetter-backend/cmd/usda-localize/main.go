package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/adapters/usda/bulkcsv"
	locapp "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocalization"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/config"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/localization/tr"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database"
	locdb "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/foodlocalization"
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("USDA localization failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: usda-localize <generate|load> [flags]")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch os.Args[1] {
	case "generate":
		return runGenerate(ctx, os.Args[2:])
	case "load":
		return runLoad(ctx, os.Args[2:])
	default:
		return fmt.Errorf("unknown subcommand %q: expected generate or load", os.Args[1])
	}
}

func runGenerate(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	datasetDirectory := flags.String("dataset-dir", "", "path to the extracted USDA FoodData Central CSV directory")
	datasetDate := flags.String("dataset-date", "", "authoritative dataset cutoff date in YYYY-MM-DD format")
	output := flags.String("output", "", "JSONL artifact output path")
	manifestPath := flags.String("manifest", "", "artifact manifest output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *datasetDate != locapp.DatasetDate {
		return fmt.Errorf("Phase 4.5 requires --dataset-date %s", locapp.DatasetDate)
	}
	if _, err := time.Parse("2006-01-02", *datasetDate); err != nil {
		return fmt.Errorf("parse --dataset-date: %w", err)
	}
	if *datasetDirectory == "" || *output == "" || *manifestPath == "" {
		return fmt.Errorf("--dataset-dir, --output, and --manifest are required")
	}
	outputAbsolute, err := filepath.Abs(*output)
	if err != nil {
		return fmt.Errorf("resolve --output: %w", err)
	}
	manifestAbsolute, err := filepath.Abs(*manifestPath)
	if err != nil {
		return fmt.Errorf("resolve --manifest: %w", err)
	}
	if outputAbsolute == manifestAbsolute {
		return fmt.Errorf("--output and --manifest must be different files")
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "eatbetter-usda-localize")
	catalog, err := bulkcsv.ReadLocalizationCatalog(ctx, *datasetDirectory, logger)
	if err != nil {
		return err
	}
	if err := validateApril2026Baseline(catalog.Candidates); err != nil {
		return err
	}
	records := make([]locapp.Record, 0, len(catalog.Candidates))
	translator := tr.Translator{}
	for _, candidate := range catalog.Candidates {
		record := translator.Translate(candidate)
		if err := record.Validate(); err != nil {
			return fmt.Errorf("validate generated USDA FDC ID %s: %w", candidate.ExternalID, err)
		}
		records = append(records, record)
	}
	artifactHash, err := locapp.WriteJSONL(*output, records)
	if err != nil {
		return err
	}
	inputFiles := make([]locapp.InputFile, 0, 3)
	for _, name := range []string{"food.csv", "food_nutrient.csv", "nutrient.csv"} {
		hash, err := locapp.HashFile(filepath.Join(*datasetDirectory, name))
		if err != nil {
			return fmt.Errorf("hash USDA input %s: %w", name, err)
		}
		inputFiles = append(inputFiles, locapp.InputFile{Name: name, SHA256: hash})
	}
	manifest := locapp.NewManifest(*datasetDate, tr.RulesetVersion, artifactHash, inputFiles, records)
	if err := locapp.WriteManifest(*manifestPath, manifest); err != nil {
		return err
	}
	return writeJSON(os.Stdout, manifest)
}

func runLoad(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("load", flag.ContinueOnError)
	artifactPath := flags.String("artifact", "", "JSONL localization artifact path")
	manifestPath := flags.String("manifest", "", "artifact manifest path")
	dryRun := flags.Bool("dry-run", false, "validate and execute the merge transaction, then roll it back")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *artifactPath == "" || *manifestPath == "" {
		return fmt.Errorf("--artifact and --manifest are required")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	result, err := (locdb.Loader{Pool: pool}).Load(ctx, *artifactPath, *manifestPath, *dryRun)
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, result)
}

func validateApril2026Baseline(candidates []locapp.Candidate) error {
	expected := map[string]int{"foundation_food": 428, "survey_fndds_food": 5431, "sr_legacy_food": 7793}
	actual := make(map[string]int)
	for _, candidate := range candidates {
		actual[candidate.DataType]++
	}
	if len(candidates) != 13_652 {
		return fmt.Errorf("April 2026 generic baseline changed: got %d candidates, want 13652", len(candidates))
	}
	for dataType, count := range expected {
		if actual[dataType] != count {
			return fmt.Errorf("April 2026 %s baseline changed: got %d candidates, want %d", dataType, actual[dataType], count)
		}
	}
	return nil
}

func writeJSON(output *os.File, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
