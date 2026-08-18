package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/adapters/usda/bulkcsv"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/config"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database"
	foodimportdb "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/foodimport"
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("USDA import failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	datasetDirectory := flag.String("dataset-dir", "", "path to the extracted USDA FoodData Central CSV directory")
	datasetDateRaw := flag.String("dataset-date", "", "authoritative dataset cutoff date in YYYY-MM-DD format")
	flag.Parse()

	datasetDate, err := time.Parse("2006-01-02", *datasetDateRaw)
	if err != nil {
		return fmt.Errorf("parse --dataset-date: expected YYYY-MM-DD: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).With(
		"service", "eatbetter-usda-import",
		"environment", cfg.AppEnvironment,
		"dataset_date", datasetDate.Format("2006-01-02"),
	)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	result, err := (bulkcsv.Importer{
		DatasetDir:  *datasetDirectory,
		DatasetDate: datasetDate,
		Logger:      logger,
		Stages:      foodimportdb.Factory{Pool: pool},
	}).Run(ctx)
	if err != nil {
		return err
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	logger.Info("USDA import result",
		"duration", result.Duration.Round(time.Millisecond),
		"diagnostics", result.Diagnostics,
		"merge", result.Merged,
		"heap_alloc_bytes", memory.Alloc,
		"heap_sys_bytes", memory.HeapSys,
	)
	return nil
}
