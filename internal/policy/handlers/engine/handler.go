// Package engine handles engine API requests for policy evaluation.
package engine

import (
	"context"

	engineserver "github.com/dcm-project/control-plane/internal/policy/api/engine"
	"github.com/dcm-project/control-plane/internal/policy/logging"
	"github.com/dcm-project/control-plane/internal/policy/service"
)

// Handler implements the engine API
type Handler struct {
	evaluationService service.EvaluationService
}

var _ engineserver.StrictServerInterface = (*Handler)(nil)

// NewHandler creates a new engine handler
func NewHandler(evaluationService service.EvaluationService) *Handler {
	return &Handler{
		evaluationService: evaluationService,
	}
}

// EvaluateRequest evaluates a service instance request against policies
func (h *Handler) EvaluateRequest(ctx context.Context, request engineserver.EvaluateRequestRequestObject) (engineserver.EvaluateRequestResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("EvaluateRequest received")

	// Convert API request to service request
	evaluationRequest, err := toServiceEvaluationRequest(request)
	if err != nil {
		log.Warn("EvaluateRequest invalid input", "error", err)
		return h.badRequest(err.Error()), nil
	}

	// Call evaluation service
	response, err := h.evaluationService.EvaluateRequest(ctx, evaluationRequest)
	if err != nil {
		logServiceError(ctx, "EvaluateRequest failed", err)
		return h.handleError(err), nil
	}

	log.Info("EvaluateRequest completed",
		"status", response.Status,
		"selected_provider", response.SelectedProvider,
	)

	// Map service response to API response
	return engineserver.EvaluateRequest200JSONResponse(toEngineEvaluationResponse(response)), nil
}
