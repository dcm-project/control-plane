// Package engineserver provides the HTTP server for the policy evaluation engine API.
package engineserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	engineserverapi "github.com/dcm-project/control-plane/api/policy/v1alpha1/engine"
	engineserver "github.com/dcm-project/control-plane/internal/policy/api/engine"
	"github.com/dcm-project/control-plane/internal/policy/config"
	"github.com/dcm-project/control-plane/internal/policy/logging"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const gracefulShutdownTimeout = 5 * time.Second

// Server wraps the HTTP server for the engine API
type Server struct {
	config   *config.Config
	listener net.Listener
	handler  engineserver.StrictServerInterface
}

// New creates a new engine server instance
func New(cfg *config.Config, listener net.Listener, handler engineserver.StrictServerInterface) *Server {
	return &Server{
		config:   cfg,
		listener: listener,
		handler:  handler,
	}
}

// Run starts the HTTP server and blocks until shutdown
func (s *Server) Run(ctx context.Context) error {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(logging.RequestLogger)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	swagger, err := engineserverapi.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to load swagger spec: %w", err)
	}

	baseURL := ""
	if len(swagger.Servers) > 0 {
		baseURL = swagger.Servers[0].URL
	}

	engineserver.HandlerFromMuxWithBaseURL(
		engineserver.NewStrictHandler(s.handler, nil),
		router,
		baseURL,
	)

	srv := &http.Server{Handler: router}

	go func() {
		<-ctx.Done()
		ctxTimeout, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		srv.SetKeepAlivesEnabled(false)
		slog.Info("Shutting down engine API server")
		if err := srv.Shutdown(ctxTimeout); err != nil {
			slog.Error("Error during engine API server shutdown", "error", err)
		}
	}()

	slog.Info("Engine API server started", "address", s.listener.Addr().String())
	if err := srv.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to serve engine API server: %w", err)
	}

	slog.Info("Engine API server stopped")
	return nil
}
