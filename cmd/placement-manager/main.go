package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/dcm-project/control-plane/internal/placement/apiserver"
	"github.com/dcm-project/control-plane/internal/placement/config"
	v1alpha1 "github.com/dcm-project/control-plane/internal/placement/handlers/v1alpha1"
	"github.com/dcm-project/control-plane/internal/placement/logging"
	"github.com/dcm-project/control-plane/internal/placement/policy"
	"github.com/dcm-project/control-plane/internal/placement/service"
	"github.com/dcm-project/control-plane/internal/placement/sprm"
	"github.com/dcm-project/control-plane/internal/placement/store"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		return 1
	}

	logging.Init(cfg.Service.LogLevel)

	slog.Info("Configuration loaded",
		"bind_address", cfg.Service.Address,
		"db_type", cfg.Database.Type,
		"db_host", cfg.Database.Hostname,
		"db_name", cfg.Database.Name,
		"log_level", cfg.Service.LogLevel,
	)

	db, err := store.InitDB(cfg)
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		return 1
	}

	// Initialize store
	dataStore := store.NewStore(db)
	defer func() {
		if err := dataStore.Close(); err != nil {
			slog.Error("Failed to close data store", "error", err)
		}
	}()

	policyClient, err := policy.NewClient(cfg.PolicyEvaluation.URL, cfg.PolicyEvaluation.Timeout)
	if err != nil {
		slog.Error("Failed to initialize policy client", "error", err)
		return 1
	}
	slog.Info("Policy client initialized",
		"url", cfg.PolicyEvaluation.URL,
		"timeout", cfg.PolicyEvaluation.Timeout,
	)

	sprmClient, err := sprm.NewClient(cfg.SPRM.URL, cfg.SPRM.Timeout)
	if err != nil {
		slog.Error("Failed to initialize SPRM client", "error", err)
		return 1
	}
	slog.Info("SPRM client initialized",
		"url", cfg.SPRM.URL,
		"timeout", cfg.SPRM.Timeout,
	)

	placementService := service.NewPlacementService(dataStore, policyClient, sprmClient)

	listener, err := net.Listen("tcp", cfg.Service.Address)
	if err != nil {
		slog.Error("Failed to create listener", "error", err)
		return 1
	}

	handler := v1alpha1.NewHandler(placementService)

	srv := apiserver.New(cfg, listener, handler)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := srv.Run(ctx); err != nil {
		slog.Error("Server failed", "error", err)
		return 1
	}

	return 0
}
