package v1alpha1

import (
	"context"

	"github.com/dcm-project/control-plane/api/policy/v1alpha1"
	"github.com/dcm-project/control-plane/internal/policy/api/server"
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
	case service.ErrorTypeInvalidArgument, service.ErrorTypeNotFound,
		service.ErrorTypeAlreadyExists, service.ErrorTypeFailedPrecondition:
		return true
	default:
		return false
	}
}

// Error handling helpers

func (h *PolicyHandler) handleCreatePolicyError(err error, _ server.CreatePolicyRequestObject) server.CreatePolicyResponseObject {
	serviceErr, ok := err.(*service.ServiceError)
	if !ok {
		return server.CreatePolicy500JSONResponse{
			InternalServerErrorJSONResponse: internalErrorResponse(buildErrorResponse(
				500,
				v1alpha1.INTERNAL,
				"Internal server error",
				strPtr(err.Error()),
			)),
		}
	}

	switch serviceErr.Type {
	case service.ErrorTypeInvalidArgument:
		return server.CreatePolicy400JSONResponse{
			BadRequestJSONResponse: badRequestResponse(buildErrorResponse(
				400,
				v1alpha1.INVALIDARGUMENT,
				serviceErr.Message,
				strPtr(serviceErr.Detail),
			)),
		}
	case service.ErrorTypeAlreadyExists:
		return server.CreatePolicy409JSONResponse{
			AlreadyExistsJSONResponse: alreadyExistsResponse(buildErrorResponse(
				409,
				v1alpha1.ALREADYEXISTS,
				serviceErr.Message,
				strPtr(serviceErr.Detail),
			)),
		}
	default:
		return server.CreatePolicy500JSONResponse{
			InternalServerErrorJSONResponse: internalErrorResponse(buildErrorResponse(
				500,
				v1alpha1.INTERNAL,
				"Internal server error",
				strPtr(serviceErr.Detail),
			)),
		}
	}
}

func (h *PolicyHandler) handleGetPolicyError(err error, _ server.GetPolicyRequestObject) server.GetPolicyResponseObject {
	serviceErr, ok := err.(*service.ServiceError)
	if !ok {
		return server.GetPolicy500JSONResponse{
			InternalServerErrorJSONResponse: internalErrorResponse(buildErrorResponse(
				500,
				v1alpha1.INTERNAL,
				"Internal server error",
				strPtr(err.Error()),
			)),
		}
	}

	switch serviceErr.Type {
	case service.ErrorTypeNotFound:
		return server.GetPolicy404JSONResponse{
			NotFoundJSONResponse: notFoundResponse(buildErrorResponse(
				404,
				v1alpha1.NOTFOUND,
				serviceErr.Message,
				strPtr(serviceErr.Detail),
			)),
		}
	default:
		return server.GetPolicy500JSONResponse{
			InternalServerErrorJSONResponse: internalErrorResponse(buildErrorResponse(
				500,
				v1alpha1.INTERNAL,
				"Internal server error",
				strPtr(serviceErr.Detail),
			)),
		}
	}
}

func (h *PolicyHandler) handleListPoliciesError(err error, _ server.ListPoliciesRequestObject) server.ListPoliciesResponseObject {
	serviceErr, ok := err.(*service.ServiceError)
	if !ok {
		return server.ListPolicies500JSONResponse{
			InternalServerErrorJSONResponse: internalErrorResponse(buildErrorResponse(
				500,
				v1alpha1.INTERNAL,
				"Internal server error",
				strPtr(err.Error()),
			)),
		}
	}

	switch serviceErr.Type {
	case service.ErrorTypeInvalidArgument:
		return server.ListPolicies400JSONResponse{
			BadRequestJSONResponse: badRequestResponse(buildErrorResponse(
				400,
				v1alpha1.INVALIDARGUMENT,
				serviceErr.Message,
				strPtr(serviceErr.Detail),
			)),
		}
	default:
		return server.ListPolicies500JSONResponse{
			InternalServerErrorJSONResponse: internalErrorResponse(buildErrorResponse(
				500,
				v1alpha1.INTERNAL,
				"Internal server error",
				strPtr(serviceErr.Detail),
			)),
		}
	}
}

func (h *PolicyHandler) handleUpdatePolicyError(err error, _ server.UpdatePolicyRequestObject) server.UpdatePolicyResponseObject {
	serviceErr, ok := err.(*service.ServiceError)
	if !ok {
		return server.UpdatePolicy500JSONResponse{
			InternalServerErrorJSONResponse: internalErrorResponse(buildErrorResponse(
				500,
				v1alpha1.INTERNAL,
				"Internal server error",
				strPtr(err.Error()),
			)),
		}
	}

	switch serviceErr.Type {
	case service.ErrorTypeInvalidArgument, service.ErrorTypeFailedPrecondition:
		return server.UpdatePolicy400JSONResponse{
			BadRequestJSONResponse: badRequestResponse(buildErrorResponse(
				400,
				v1alpha1.INVALIDARGUMENT,
				serviceErr.Message,
				strPtr(serviceErr.Detail),
			)),
		}
	case service.ErrorTypeNotFound:
		return server.UpdatePolicy404JSONResponse{
			NotFoundJSONResponse: notFoundResponse(buildErrorResponse(
				404,
				v1alpha1.NOTFOUND,
				serviceErr.Message,
				strPtr(serviceErr.Detail),
			)),
		}
	case service.ErrorTypeAlreadyExists:
		return server.UpdatePolicy409JSONResponse{
			AlreadyExistsJSONResponse: alreadyExistsResponse(buildErrorResponse(
				409,
				v1alpha1.ALREADYEXISTS,
				serviceErr.Message,
				strPtr(serviceErr.Detail),
			)),
		}
	default:
		return server.UpdatePolicy500JSONResponse{
			InternalServerErrorJSONResponse: internalErrorResponse(buildErrorResponse(
				500,
				v1alpha1.INTERNAL,
				"Internal server error",
				strPtr(serviceErr.Detail),
			)),
		}
	}
}

func (h *PolicyHandler) handleDeletePolicyError(err error, _ server.DeletePolicyRequestObject) server.DeletePolicyResponseObject {
	serviceErr, ok := err.(*service.ServiceError)
	if !ok {
		return server.DeletePolicy500JSONResponse{
			InternalServerErrorJSONResponse: internalErrorResponse(buildErrorResponse(
				500,
				v1alpha1.INTERNAL,
				"Internal server error",
				strPtr(err.Error()),
			)),
		}
	}

	switch serviceErr.Type {
	case service.ErrorTypeNotFound:
		return server.DeletePolicy404JSONResponse{
			NotFoundJSONResponse: notFoundResponse(buildErrorResponse(
				404,
				v1alpha1.NOTFOUND,
				serviceErr.Message,
				strPtr(serviceErr.Detail),
			)),
		}
	default:
		return server.DeletePolicy500JSONResponse{
			InternalServerErrorJSONResponse: internalErrorResponse(buildErrorResponse(
				500,
				v1alpha1.INTERNAL,
				"Internal server error",
				strPtr(serviceErr.Detail),
			)),
		}
	}
}

// buildErrorResponse builds an RFC 7807 error response
func buildErrorResponse(status int32, errorType v1alpha1.ErrorType, title string, detail *string) v1alpha1.Error {
	return v1alpha1.Error{
		Status: status,
		Type:   errorType,
		Title:  title,
		Detail: detail,
	}
}

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}

// Typed error response helpers
func badRequestResponse(e v1alpha1.Error) server.BadRequestJSONResponse {
	return server.BadRequestJSONResponse(e)
}

func notFoundResponse(e v1alpha1.Error) server.NotFoundJSONResponse {
	return server.NotFoundJSONResponse(e)
}

func alreadyExistsResponse(e v1alpha1.Error) server.AlreadyExistsJSONResponse {
	return server.AlreadyExistsJSONResponse(e)
}

func internalErrorResponse(e v1alpha1.Error) server.InternalServerErrorJSONResponse {
	return server.InternalServerErrorJSONResponse(e)
}
