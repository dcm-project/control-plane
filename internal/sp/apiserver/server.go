// Package apiserver configures and runs the HTTP server and router.
package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	providerapi "github.com/dcm-project/control-plane/api/sp/v1alpha1/provider"
	"github.com/dcm-project/control-plane/api/sp/v1alpha1/resource_manager"
	providerserver "github.com/dcm-project/control-plane/internal/sp/api/provider"
	rmserver "github.com/dcm-project/control-plane/internal/sp/api/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/config"
	"github.com/dcm-project/control-plane/internal/sp/logging"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
)

const gracefulShutdownTimeout = 5 * time.Second

type Server struct {
	cfg             *config.Config
	listener        net.Listener
	providerHandler providerserver.StrictServerInterface
	rmHandler       rmserver.StrictServerInterface
}

func New(cfg *config.Config, listener net.Listener, providerHandler providerserver.StrictServerInterface, rmHandler rmserver.StrictServerInterface) *Server {
	return &Server{
		cfg:             cfg,
		listener:        listener,
		providerHandler: providerHandler,
		rmHandler:       rmHandler,
	}
}

func (s *Server) Run(ctx context.Context) error {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(logging.RequestLogger)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	// Load both OpenAPI specs for validation
	providerSwagger, err := providerapi.GetSwagger()
	if err != nil {
		return fmt.Errorf("load Provider OpenAPI spec: %w", err)
	}

	rmSwagger, err := resource_manager.GetSwagger()
	if err != nil {
		return fmt.Errorf("load Resource Manager OpenAPI spec: %w", err)
	}

	// Create /api/v1alpha1 router
	apiRouter := chi.NewRouter()

	// Add smart validation middleware that routes to the correct validator
	apiRouter.Use(s.validationMiddleware(providerSwagger, rmSwagger))

	// Register both handler sets
	providerserver.HandlerFromMux(
		providerserver.NewStrictHandler(s.providerHandler, nil),
		apiRouter,
	)

	rmserver.HandlerFromMux(
		rmserver.NewStrictHandler(s.rmHandler, nil),
		apiRouter,
	)

	// Mount the API router
	router.Mount("/api/v1alpha1", apiRouter)

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

// validationMiddleware returns middleware that validates requests against the appropriate OpenAPI spec
func (s *Server) validationMiddleware(providerSwagger, rmSwagger *openapi3.T) func(http.Handler) http.Handler {
	// Create validators once for better performance
	providerValidator := nethttpmiddleware.OapiRequestValidatorWithOptions(providerSwagger, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
		SilenceServersWarning: true,
	})

	rmValidator := nethttpmiddleware.OapiRequestValidatorWithOptions(rmSwagger, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
		SilenceServersWarning: true,
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Route to the appropriate validator based on path
			if strings.HasPrefix(r.URL.Path, "/api/v1alpha1/service-type-instances") {
				rmValidator(next).ServeHTTP(w, r)
			} else {
				providerValidator(next).ServeHTTP(w, r)
			}
		})
	}
}
