// Package apiserver provides the HTTP server setup and lifecycle management.
package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	"github.com/dcm-project/control-plane/internal/catalog/config"
	"github.com/dcm-project/control-plane/internal/catalog/logging"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
)

const gracefulShutdownTimeout = 5 * time.Second

type Server struct {
	config   *config.Config
	listener net.Listener
	handler  server.StrictServerInterface
	logger   *slog.Logger
}

func New(cfg *config.Config, listener net.Listener, handler server.StrictServerInterface, logger *slog.Logger) *Server {
	return &Server{
		config:   cfg,
		listener: listener,
		handler:  handler,
		logger:   logger.With("component", "apiserver"),
	}
}

func (s *Server) Run(ctx context.Context) error {
	router := chi.NewRouter()
	router.Use(logging.Middleware(s.logger))
	router.Use(middleware.Recoverer)

	swagger, err := v1alpha1.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to load swagger spec: %w", err)
	}

	baseURL := ""
	if len(swagger.Servers) > 0 {
		baseURL = swagger.Servers[0].URL
	}

	// Add OpenAPI request validation middleware
	router.Use(nethttpmiddleware.OapiRequestValidatorWithOptions(swagger, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
		SilenceServersWarning: true,
	}))

	// Mount the generated handler with base URL from OpenAPI spec
	server.HandlerFromMuxWithBaseURL(
		server.NewStrictHandler(s.handler, nil),
		router,
		baseURL,
	)

	srv := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: s.config.Service.ReadHeaderTimeout,
		ReadTimeout:       s.config.Service.ReadTimeout,
		WriteTimeout:      s.config.Service.WriteTimeout,
		IdleTimeout:       s.config.Service.IdleTimeout,
		MaxHeaderBytes:    s.config.Service.MaxHeaderBytes,
	}

	go func() {
		<-ctx.Done()
		ctxTimeout, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		srv.SetKeepAlivesEnabled(false)
		s.logger.Info("Shutting down server")
		_ = srv.Shutdown(ctxTimeout)
	}()

	s.logger.Info("Starting server", "address", s.listener.Addr().String())
	if err := srv.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	s.logger.Info("Server stopped")
	return nil
}
