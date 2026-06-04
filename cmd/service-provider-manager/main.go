package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	apiserver "github.com/dcm-project/control-plane/internal/sp/apiserver"
	"github.com/dcm-project/control-plane/internal/sp/cleanup"
	"github.com/dcm-project/control-plane/internal/sp/config"
	"github.com/dcm-project/control-plane/internal/sp/consumer"
	providerhandler "github.com/dcm-project/control-plane/internal/sp/handlers/provider"
	rmhandlers "github.com/dcm-project/control-plane/internal/sp/handlers/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/healthcheck"
	"github.com/dcm-project/control-plane/internal/sp/logging"
	providersvc "github.com/dcm-project/control-plane/internal/sp/service/provider"
	rmsvc "github.com/dcm-project/control-plane/internal/sp/service/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/store"
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

	// Initialize store, service, and handler
	dataStore := store.NewStore(db)
	defer func() {
		if err := dataStore.Close(); err != nil {
			slog.Error("Failed to close data store", "error", err)
		}
	}()

	// Provider API
	providerService := providersvc.NewProviderService(dataStore)
	providerHandler := providerhandler.NewHandler(providerService)

	// Resource Manager API
	instanceService := rmsvc.NewInstanceService(dataStore, nil)
	rmHandler := rmhandlers.NewHandler(instanceService)

	// Initialize StatusConsumer
	statusConsumer, err := consumer.New(cfg.NATS.URL, cfg.NATS.Subject, dataStore,
		consumer.SetStreamName(cfg.NATS.StreamName),
		consumer.SetConsumerName(cfg.NATS.ConsumerName),
	)
	if err != nil {
		slog.Error("Failed to initialize status consumer", "error", err)
		return 1
	}
	slog.Info("Status consumer initialized",
		"nats_url", cfg.NATS.URL,
		"stream", cfg.NATS.StreamName,
		"consumer", cfg.NATS.ConsumerName,
	)

	// Start server
	listener, err := net.Listen("tcp", cfg.Service.Address)
	if err != nil {
		slog.Error("Failed to create listener", "error", err)
		return 1
	}

	srv := apiserver.New(cfg, listener, providerHandler, rmHandler)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start StatusConsumer
	if err := statusConsumer.Start(ctx); err != nil {
		slog.Error("Failed to start status consumer", "error", err)
		return 1
	}
	defer statusConsumer.Stop()

	// Start health check monitor
	healthMonitor := healthcheck.NewMonitor(dataStore.Provider(), dataStore.ServiceTypeInstance(), cfg.HealthCheck)
	healthMonitor.Start(ctx)
	defer healthMonitor.Stop()
	slog.Info("Health check monitor started", "interval", cfg.HealthCheck.Interval)

	// Start cleanup scheduler for deferred deletions
	cleanupScheduler := cleanup.NewScheduler(dataStore, instanceService, cfg.Cleanup)
	cleanupScheduler.Start(ctx)
	defer cleanupScheduler.Stop()
	slog.Info("Cleanup scheduler started", "interval", cfg.Cleanup.Interval, "max_retries", cfg.Cleanup.MaxRetries)

	slog.Info("Starting server", "address", listener.Addr().String())
	if err := srv.Run(ctx); err != nil {
		slog.Error("Server failed", "error", err)
		return 1
	}

	return 0
}
