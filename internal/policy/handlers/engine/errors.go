package engine

import (
	"context"

	engineserver "github.com/dcm-project/control-plane/internal/policy/api/engine"
	"github.com/dcm-project/control-plane/internal/policy/logging"
	"github.com/dcm-project/control-plane/internal/policy/service"
)

// logServiceError logs at Warn level for client errors (4xx) and Error level
// for internal failures (5xx), so log severity matches HTTP response semantics.
func logServiceError(ctx context.Context, msg string, err error, attrs ...any) {
	log := logging.FromContext(ctx)
	args := append([]any{"error", err}, attrs...)
	if isClientError(err) {
		log.Warn(msg, args...)
	} else {
		log.Error(msg, args...)
	}
}

func isClientError(err error) bool {
	serviceErr, ok := err.(*service.ServiceError)
	if !ok {
		return false
	}
	switch serviceErr.Type {
	case service.ErrorTypeInvalidArgument, service.ErrorTypeRejected,
		service.ErrorTypePolicyConflict:
		return true
	default:
		return false
	}
}

// handleError maps service errors to HTTP responses
func (h *Handler) handleError(err error) engineserver.EvaluateRequestResponseObject {
	if serviceErr, ok := err.(*service.ServiceError); ok {
		switch serviceErr.Type {
		case service.ErrorTypeRejected:
			return h.rejected(serviceErr.Message, serviceErr.Detail)
		case service.ErrorTypePolicyConflict:
			return h.conflict(serviceErr.Message, serviceErr.Detail)
		case service.ErrorTypeInvalidArgument:
			return h.badRequest(serviceErr.Message)
		}
	}

	// Default to internal server error
	return h.internalError("Internal server error", "An unexpected error occurred")
}

// badRequest creates a 400 Bad Request response
func (h *Handler) badRequest(message string) engineserver.EvaluateRequestResponseObject {
	return engineserver.EvaluateRequest400JSONResponse{
		BadRequestJSONResponse: engineserver.BadRequestJSONResponse{
			Type:   "about:blank",
			Status: 400,
			Title:  "Bad Request",
			Detail: &message,
		},
	}
}

// rejected creates a 406 Not Acceptable response
func (h *Handler) rejected(title, detail string) engineserver.EvaluateRequestResponseObject {
	return engineserver.EvaluateRequest406JSONResponse{
		RejectedJSONResponse: engineserver.RejectedJSONResponse{
			Type:   "about:blank",
			Status: 406,
			Title:  title,
			Detail: &detail,
		},
	}
}

// conflict creates a 409 Conflict response
func (h *Handler) conflict(title, detail string) engineserver.EvaluateRequestResponseObject {
	return engineserver.EvaluateRequest409JSONResponse{
		PolicyConflictJSONResponse: engineserver.PolicyConflictJSONResponse{
			Type:   "about:blank",
			Status: 409,
			Title:  title,
			Detail: &detail,
		},
	}
}

// internalError creates a 500 Internal Server Error response
func (h *Handler) internalError(title, detail string) engineserver.EvaluateRequestResponseObject {
	return engineserver.EvaluateRequest500JSONResponse{
		InternalServerErrorJSONResponse: engineserver.InternalServerErrorJSONResponse{
			Type:   "about:blank",
			Status: 500,
			Title:  title,
			Detail: &detail,
		},
	}
}
