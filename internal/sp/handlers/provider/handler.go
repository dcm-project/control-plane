// Package provider implements HTTP handlers for the Provider API.
package provider

import (
	"context"

	providerserver "github.com/dcm-project/control-plane/internal/sp/api/provider"
	"github.com/dcm-project/control-plane/internal/sp/logging"
	"github.com/dcm-project/control-plane/internal/sp/service"
	providersvc "github.com/dcm-project/control-plane/internal/sp/service/provider"
)

// Handler implements the generated StrictServerInterface for the Provider API.
type Handler struct {
	providerService *providersvc.ProviderService
}

// NewHandler creates a new Handler with the given provider service.
func NewHandler(providerService *providersvc.ProviderService) *Handler {
	return &Handler{providerService: providerService}
}

// Ensure Handler implements StrictServerInterface
var _ providerserver.StrictServerInterface = (*Handler)(nil)

func (h *Handler) ListProviders(ctx context.Context, request providerserver.ListProvidersRequestObject) (providerserver.ListProvidersResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("ListProviders request received",
		"type", request.Params.Type,
		"page_size", request.Params.MaxPageSize,
	)

	var serviceType string
	var maxPageSize int
	var pageToken string

	if request.Params.Type != nil {
		serviceType = *request.Params.Type
	}
	if request.Params.MaxPageSize != nil {
		maxPageSize = *request.Params.MaxPageSize
	}
	if request.Params.PageToken != nil {
		pageToken = *request.Params.PageToken
	}

	result, err := h.providerService.ListProviders(ctx, serviceType, maxPageSize, pageToken)
	if err != nil {
		logServiceError(ctx, "ListProviders failed", err)
		if svcErr, ok := err.(*service.ServiceError); ok && svcErr.Code == service.ErrCodeValidation {
			return providerserver.ListProviders400ApplicationProblemPlusJSONResponse(newError("validation-error", "Invalid request", svcErr.Message, 400)), nil
		}
		return providerserver.ListProviders400ApplicationProblemPlusJSONResponse(newError("list-error", "Failed to list providers", err.Error(), 400)), nil
	}

	response := providerserver.ListProviders200JSONResponse{Providers: &result.Providers}
	if result.NextPageToken != "" {
		response.NextPageToken = &result.NextPageToken
	}

	log.Debug("ListProviders completed", "count", len(result.Providers))
	return response, nil
}

func (h *Handler) CreateProvider(ctx context.Context, request providerserver.CreateProviderRequestObject) (providerserver.CreateProviderResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("CreateProvider request received",
		"client_id", request.Params.Id,
		"name", request.Body.Name,
	)

	response, updated, err := h.providerService.RegisterOrUpdateProvider(ctx, request.Body, request.Params.Id)
	if err != nil {
		logServiceError(ctx, "CreateProvider failed", err)
		if svcErr, ok := err.(*service.ServiceError); ok {
			switch svcErr.Code {
			case service.ErrCodeValidation:
				return providerserver.CreateProvider400ApplicationProblemPlusJSONResponse(newError("validation-error", "Validation failed", svcErr.Message, 400)), nil
			case service.ErrCodeConflict:
				return providerserver.CreateProvider409ApplicationProblemPlusJSONResponse(newError("conflict", "Resource conflict", svcErr.Message, 409)), nil
			}
		}
		return providerserver.CreateProvider400ApplicationProblemPlusJSONResponse(newError("create-error", "Failed to create provider", err.Error(), 400)), nil
	}

	if updated {
		log.Info("Provider updated", "provider_id", *response.Id)
		return providerserver.CreateProvider200JSONResponse(*response), nil
	}
	log.Info("Provider created", "provider_id", *response.Id)
	return providerserver.CreateProvider201JSONResponse(*response), nil
}

func (h *Handler) GetProvider(ctx context.Context, request providerserver.GetProviderRequestObject) (providerserver.GetProviderResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("GetProvider request received", "provider_id", request.ProviderId)

	provider, err := h.providerService.GetProvider(ctx, request.ProviderId)
	if err != nil {
		logServiceError(ctx, "GetProvider failed", err, "provider_id", request.ProviderId)
		if svcErr, ok := err.(*service.ServiceError); ok && svcErr.Code == service.ErrCodeNotFound {
			return providerserver.GetProvider404ApplicationProblemPlusJSONResponse(newError("not-found", "Provider not found", svcErr.Message, 404)), nil
		}
		return providerserver.GetProvider400ApplicationProblemPlusJSONResponse(newError("get-error", "Failed to get provider", err.Error(), 400)), nil
	}

	log.Debug("GetProvider completed", "provider_id", request.ProviderId)
	return providerserver.GetProvider200JSONResponse(*provider), nil
}

func (h *Handler) ApplyProvider(ctx context.Context, request providerserver.ApplyProviderRequestObject) (providerserver.ApplyProviderResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("ApplyProvider request received", "provider_id", request.ProviderId)

	provider, err := h.providerService.UpdateProvider(ctx, request.ProviderId, request.Body)
	if err != nil {
		logServiceError(ctx, "ApplyProvider failed", err, "provider_id", request.ProviderId)
		if svcErr, ok := err.(*service.ServiceError); ok {
			switch svcErr.Code {
			case service.ErrCodeNotFound:
				return providerserver.ApplyProvider404ApplicationProblemPlusJSONResponse(newError("not-found", "Provider not found", svcErr.Message, 404)), nil
			case service.ErrCodeConflict:
				return providerserver.ApplyProvider409ApplicationProblemPlusJSONResponse(newError("conflict", "Name conflict", svcErr.Message, 409)), nil
			}
		}
		return providerserver.ApplyProvider400ApplicationProblemPlusJSONResponse(newError("update-error", "Failed to update provider", err.Error(), 400)), nil
	}

	log.Info("Provider updated", "provider_id", request.ProviderId)
	return providerserver.ApplyProvider200JSONResponse(*provider), nil
}

func (h *Handler) DeleteProvider(ctx context.Context, request providerserver.DeleteProviderRequestObject) (providerserver.DeleteProviderResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("DeleteProvider request received", "provider_id", request.ProviderId)

	err := h.providerService.DeleteProvider(ctx, request.ProviderId)
	if err != nil {
		logServiceError(ctx, "DeleteProvider failed", err, "provider_id", request.ProviderId)
		if svcErr, ok := err.(*service.ServiceError); ok && svcErr.Code == service.ErrCodeNotFound {
			return providerserver.DeleteProvider404ApplicationProblemPlusJSONResponse(newError("not-found", "Provider not found", svcErr.Message, 404)), nil
		}
		return providerserver.DeleteProvider400ApplicationProblemPlusJSONResponse(newError("delete-error", "Failed to delete provider", err.Error(), 400)), nil
	}

	log.Info("Provider deleted", "provider_id", request.ProviderId)
	return providerserver.DeleteProvider204Response{}, nil
}

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

func newError(errType, title, detail string, status int) providerserver.Error {
	return providerserver.Error{
		Type:   errType,
		Title:  title,
		Detail: &detail,
		Status: &status,
	}
}
