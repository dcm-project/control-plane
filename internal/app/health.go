package app

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

const monolithHealthPath = "/api/v1alpha1/health"

type healthResponse struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

func registerMonolithHealth(router chi.Router) {
	router.Get(monolithHealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(healthResponse{
			Status: "ok",
			Path:   monolithHealthPath,
		})
	})
}
