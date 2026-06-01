package v1alpha1

import (
	"context"
	"errors"

	apiv1alpha1 "github.com/dcm-project/control-plane/api/placement/v1alpha1"
	"github.com/dcm-project/control-plane/internal/placement/api/server"
	"github.com/dcm-project/control-plane/internal/placement/logging"
	"github.com/dcm-project/control-plane/internal/placement/service"
)

// logServiceError logs at Warn level for client errors (4xx) and Error level
// for internal failures (5xx), so log severity matches HTTP response semantics.
func logServiceError(ctx context.Context, msg string, err error, attrs ...any) {
	log := logging.FromContext(ctx)
	args := append([]any{"error", err}, attrs...)
	var svcErr *service.ServiceError
	if service.IsClientError(err, &svcErr) {
		log.Warn(msg, args...)
	} else {
		log.Error(msg, args...)
	}
}

// newError creates an RFC 7807 compliant error response.
func newError(errType, title, detail string, status int) apiv1alpha1.Error {
	return apiv1alpha1.Error{
		Type:   errType,
		Title:  title,
		Detail: &detail,
		Status: &status,
	}
}

// handleListResourcesError converts a service error to a ListResources response.
func handleListResourcesError(err error) server.ListResourcesResponseObject {
	var svcErr *service.ServiceError
	if errors.As(err, &svcErr) && svcErr.Code == service.ErrCodeValidation {
		return server.ListResources400ApplicationProblemPlusJSONResponse(newError("validation-error", "Invalid request", svcErr.Message, 400))
	}
	return server.ListResourcesdefaultApplicationProblemPlusJSONResponse{
		Body:       newError("list-error", "Failed to list resources", err.Error(), 500),
		StatusCode: 500,
	}
}

// handleCreateResourceError converts a service error to a CreateResource response.
func handleCreateResourceError(err error) server.CreateResourceResponseObject {
	var svcErr *service.ServiceError
	if errors.As(err, &svcErr) {
		switch svcErr.Code {
		case service.ErrCodeValidation:
			return server.CreateResource400ApplicationProblemPlusJSONResponse(newError("validation-error", "Validation failed", svcErr.Message, 400))
		case service.ErrCodePolicyRejected:
			return server.CreateResource406ApplicationProblemPlusJSONResponse(newError("policy-rejected", "Policy rejected request", svcErr.Message, 406))
		case service.ErrCodeConflict:
			return server.CreateResource409ApplicationProblemPlusJSONResponse(newError("resource-conflict", "Resource already exists", svcErr.Message, 409))
		case service.ErrCodePolicyConflict:
			return server.CreateResource409ApplicationProblemPlusJSONResponse(newError("policy-conflict", "Policy conflict", svcErr.Message, 409))
		case service.ErrCodeProviderError:
			return server.CreateResource422ApplicationProblemPlusJSONResponse(newError("provider-error", "Provider error", svcErr.Message, 422))
		case service.ErrCodeInternal, service.ErrCodePolicyError, service.ErrCodePolicyInternalError, service.ErrCodeSPRMError:
			return server.CreateResourcedefaultApplicationProblemPlusJSONResponse{
				Body:       newError("internal-error", "Internal error", svcErr.Message, 500),
				StatusCode: 500,
			}
		}
	}
	return server.CreateResourcedefaultApplicationProblemPlusJSONResponse{
		Body:       newError("create-error", "Failed to create resource", err.Error(), 500),
		StatusCode: 500,
	}
}

// handleGetResourceError converts a service error to a GetResource response.
func handleGetResourceError(err error) server.GetResourceResponseObject {
	var svcErr *service.ServiceError
	if errors.As(err, &svcErr) {
		switch svcErr.Code {
		case service.ErrCodeValidation:
			return server.GetResource400ApplicationProblemPlusJSONResponse(newError("validation-error", "Invalid request", svcErr.Message, 400))
		case service.ErrCodeNotFound:
			return server.GetResource404ApplicationProblemPlusJSONResponse(newError("not-found", "Resource not found", svcErr.Message, 404))
		}
	}
	return server.GetResourcedefaultApplicationProblemPlusJSONResponse{
		Body:       newError("get-error", "Failed to get resource", err.Error(), 500),
		StatusCode: 500,
	}
}

// handleRehydrateResourceError converts a service error to a RehydrateResource response.
func handleRehydrateResourceError(err error) server.RehydrateResourceResponseObject {
	var svcErr *service.ServiceError
	if errors.As(err, &svcErr) {
		switch svcErr.Code {
		case service.ErrCodeValidation:
			return server.RehydrateResource400ApplicationProblemPlusJSONResponse(newError("validation-error", "Validation failed", svcErr.Message, 400))
		case service.ErrCodeNotFound:
			return server.RehydrateResource404ApplicationProblemPlusJSONResponse(newError("not-found", "Resource not found", svcErr.Message, 404))
		case service.ErrCodePolicyRejected:
			return server.RehydrateResource406ApplicationProblemPlusJSONResponse(newError("policy-rejected", "Policy rejected request", svcErr.Message, 406))
		case service.ErrCodeConflict, service.ErrCodePolicyConflict:
			return server.RehydrateResource409ApplicationProblemPlusJSONResponse(newError("resource-conflict", "Resource conflict", svcErr.Message, 409))
		case service.ErrCodeProviderError:
			return server.RehydrateResource422ApplicationProblemPlusJSONResponse(newError("provider-error", "Provider error", svcErr.Message, 422))
		case service.ErrCodeInternal, service.ErrCodePolicyError, service.ErrCodePolicyInternalError, service.ErrCodeSPRMError:
			return server.RehydrateResourcedefaultApplicationProblemPlusJSONResponse{
				Body:       newError("internal-error", "Internal error", svcErr.Message, 500),
				StatusCode: 500,
			}
		}
	}
	return server.RehydrateResourcedefaultApplicationProblemPlusJSONResponse{
		Body:       newError("rehydrate-error", "Failed to rehydrate resource", err.Error(), 500),
		StatusCode: 500,
	}
}

// handleDeleteResourceError converts a service error to a DeleteResource response.
func handleDeleteResourceError(err error) server.DeleteResourceResponseObject {
	var svcErr *service.ServiceError
	if errors.As(err, &svcErr) {
		switch svcErr.Code {
		case service.ErrCodeValidation:
			return server.DeleteResource400ApplicationProblemPlusJSONResponse(newError("validation-error", "Invalid request", svcErr.Message, 400))
		case service.ErrCodeNotFound:
			return server.DeleteResource404ApplicationProblemPlusJSONResponse(newError("not-found", "Resource not found", svcErr.Message, 404))
		case service.ErrCodeProviderError:
			return server.DeleteResource422ApplicationProblemPlusJSONResponse(newError("provider-error", "Provider error", svcErr.Message, 422))
		case service.ErrCodeInternal, service.ErrCodeSPRMError:
			return server.DeleteResourcedefaultApplicationProblemPlusJSONResponse{
				Body:       newError("internal-error", "Internal error", svcErr.Message, 500),
				StatusCode: 500,
			}
		}
	}
	return server.DeleteResourcedefaultApplicationProblemPlusJSONResponse{
		Body:       newError("delete-error", "Failed to delete resource", err.Error(), 500),
		StatusCode: 500,
	}
}
