package logging

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLogger is a Chi middleware that creates a per-request logger
// with a "request_id" attribute and stores it in the request context.
// It relies on chi's middleware.RequestID to generate the ID.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := middleware.GetReqID(r.Context())
		logger := slog.Default().With("request_id", requestID)
		ctx := WithLogger(r.Context(), logger)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
