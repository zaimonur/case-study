package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/config"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/foodalias"
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("food alias materialization failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).With(
		"service", "eatbetter-food-alias-materialize",
		"environment", cfg.AppEnvironment,
	)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	result, err := foodalias.Materialize(ctx, pool)
	if err != nil {
		return err
	}
	logger.Info("food alias materialization result",
		"materializer", foodalias.TurkishChickenMaterializer,
		"selected_food_ids", result.SelectedIDs,
		"inserted", result.Inserted,
		"deleted", result.Deleted,
	)
	return nil
}
