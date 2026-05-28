package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dcm-project/control-plane/internal/catalog/apiserver"
	"github.com/dcm-project/control-plane/internal/catalog/config"
	"github.com/dcm-project/control-plane/internal/catalog/handlers/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/placement"
	"github.com/dcm-project/control-plane/internal/catalog/service"
	"github.com/dcm-project/control-plane/internal/catalog/store"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		return 1
	}

	// Initialize structured logger
	logger := initLogger(cfg.Service.LogLevel)
	slog.SetDefault(logger)

	logger.Info("Configuration loaded",
		"bind_address", cfg.Service.BindAddress,
		"db_type", cfg.Database.Type,
		"db_host", cfg.Database.Hostname,
		"db_name", cfg.Database.Name,
		"log_level", cfg.Service.LogLevel,
	)

	// Initialize database
	db, err := store.InitDB(cfg, logger)
	if err != nil {
		logger.Error("Failed to initialize database", "error", err)
		return 1
	}

	// Create store
	dataStore := store.NewStore(db, logger)
	defer func() {
		if err := dataStore.Close(); err != nil {
			logger.Error("Failed to close database", "error", err)
		}
	}()

	// Create Placement Manager client
	pmClient, err := placement.NewClient(cfg.Placement.URL, cfg.Placement.Timeout, logger)
	if err != nil {
		logger.Error("Failed to create placement manager client", "error", err)
		return 1
	}

	// Create context with signal handling (used for seed and server)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create service layer
	svc, err := service.NewService(dataStore, pmClient, cfg.Seed, logger)
	if err != nil {
		logger.Error("Failed to create service", "error", err)
		return 1
	}

	// Seed service types and default catalog items if empty
	if err := svc.Seed(ctx); err != nil {
		logger.Error("Failed to seed database", "error", err)
		return 1
	}

	// Create TCP listener
	listener, err := net.Listen("tcp", cfg.Service.BindAddress)
	if err != nil {
		logger.Error("Failed to create listener", "error", err)
		return 1
	}
	defer func() { _ = listener.Close() }()

	srv := apiserver.New(cfg, listener, v1alpha1.NewHandler(svc, logger), logger)

	// Run server
	if err := srv.Run(ctx); err != nil {
		logger.Error("Server failed", "error", err)
		return 1
	}

	return 0
}

func initLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
		// Create a temporary logger to warn about the unrecognized level before
		// returning the final logger with the default level.
		tmp := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
		tmp.Warn("Unrecognized log level, defaulting to info", "level", level)
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	return slog.New(handler)
}
