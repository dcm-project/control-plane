// Package apiserver provides the HTTP server for the placement API.
package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/dcm-project/control-plane/api/placement/v1alpha1"
	"github.com/dcm-project/control-plane/internal/placement/api/server"
	"github.com/dcm-project/control-plane/internal/placement/config"
	"github.com/dcm-project/control-plane/internal/placement/logging"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
)

const gracefulShutdownTimeout = 5 * time.Second

type Server struct {
	cfg      *config.Config
	listener net.Listener
	handler  server.StrictServerInterface
}

func New(cfg *config.Config, listener net.Listener, handler server.StrictServerInterface) *Server {
	return &Server{
		cfg:      cfg,
		listener: listener,
		handler:  handler,
	}
}

func (s *Server) Run(ctx context.Context) error {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(logging.RequestLogger)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	// Placement API
	swagger, err := v1alpha1.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to load OpenAPI spec: %w", err)
	}
	if len(swagger.Servers) == 0 {
		return fmt.Errorf("OpenAPI spec missing servers configuration")
	}

	// Add OpenAPI request validation middleware
	router.Use(nethttpmiddleware.OapiRequestValidatorWithOptions(swagger, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
		SilenceServersWarning: true,
	}))

	server.HandlerFromMuxWithBaseURL(server.NewStrictHandler(s.handler, nil), router, swagger.Servers[0].URL)

	srv := http.Server{Handler: router}

	go func() {
		<-ctx.Done()
		ctxTimeout, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		srv.SetKeepAlivesEnabled(false)
		slog.Info("Shutting down server")
		_ = srv.Shutdown(ctxTimeout)
	}()

	slog.Info("Starting server", "address", s.listener.Addr().String())
	if err := srv.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	slog.Info("Server stopped")
	return nil
}
