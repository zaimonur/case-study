package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"eatbetter-backend/internal/config"
	"eatbetter-backend/internal/httpapi"
	"eatbetter-backend/internal/platform/database"
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

	handler := httpapi.NewRouter(logger, cfg.Database.PingTimeout, pool.Ping)
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
