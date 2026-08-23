package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/adapters/gemini"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/adapters/groq"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodimageextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodresolver"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealai"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/nutritioncalc"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/config"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/httpapi"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database"
	dbfooddetail "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/fooddetail"
	dbfoodsearch "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/foodsearch"
	dbnutritioncalc "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/nutritioncalc"
)

func main() {
	if err := run(); err != nil {
		bootstrapLogger().Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).With(
		"service", "eatbetter-api",
		"environment", cfg.AppEnvironment,
	)
	slog.SetDefault(logger)

	lifecycleContext, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	pool, err := database.Open(lifecycleContext, cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	logger.Info("database connection established")

	foodSearchService := foodsearch.NewService(dbfoodsearch.New(pool))
	foodDetailService := fooddetail.NewService(dbfooddetail.New(pool))
	nutritionService := nutritioncalc.NewService(dbnutritioncalc.New(pool))
	foodResolverService := foodresolver.NewService(foodSearchService)
	foodAmountService := foodamount.NewService(foodDetailService)
	var textExtractor foodextraction.Extractor
	if strings.TrimSpace(cfg.Groq.APIKey) != "" {
		textExtractor, err = groq.NewExtractor(cfg.Groq)
		if err != nil {
			return fmt.Errorf("construct Groq text extractor: %w", err)
		}
	}
	extractionService := foodextraction.NewService(textExtractor)
	var imageExtractor foodimageextraction.Extractor
	if strings.TrimSpace(cfg.Gemini.APIKey) != "" {
		imageExtractor, err = gemini.NewExtractor(cfg.Gemini)
		if err != nil {
			return fmt.Errorf("construct Gemini image extractor: %w", err)
		}
	}
	imageExtractionService := foodimageextraction.NewService(imageExtractor)
	mealAIService := mealai.NewService(
		extractionService, imageExtractionService,
		foodResolverService, foodAmountService, foodDetailService, nutritionService,
	)
	handler := httpapi.NewRouter(
		logger, cfg.Database.PingTimeout, pool.Ping,
		foodSearchService, foodDetailService, nutritionService, mealAIService,
	)
	server := httpapi.NewServer(cfg.HTTP, handler)
	serverErrors := make(chan error, 1)

	go func() {
		logger.Info("HTTP server started", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-lifecycleContext.Done():
		logger.Info("shutdown signal received")
	case serverError := <-serverErrors:
		if !errors.Is(serverError, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", serverError)
		}
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownContext); err != nil {
		_ = server.Close()
		return fmt.Errorf("gracefully shut down HTTP server: %w", err)
	}

	logger.Info("HTTP server stopped; closing database pool")
	return nil
}

func bootstrapLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "eatbetter-api")
}
