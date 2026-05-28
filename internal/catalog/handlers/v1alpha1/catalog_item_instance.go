package v1alpha1

import (
	"context"

	v1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	"github.com/dcm-project/control-plane/internal/catalog/service"
)

func (h *Handler) ListCatalogItemInstances(ctx context.Context, request server.ListCatalogItemInstancesRequestObject) (server.ListCatalogItemInstancesResponseObject, error) {
	h.logger.DebugContext(ctx, "Listing catalog item instances")

	// Build service request from HTTP params
	opts := service.CatalogItemInstanceListOptions{
		PageToken:     request.Params.PageToken,
		MaxPageSize:   request.Params.MaxPageSize,
		CatalogItemId: request.Params.CatalogItemId,
	}

	// Call service layer
	result, err := h.service.CatalogItemInstance().List(ctx, opts)
	if err != nil {
		if isInvalidPageToken(err) {
			h.logger.WarnContext(ctx, "Invalid page_token", "error", err)
			return server.ListCatalogItemInstances400JSONResponse{
				BadRequestJSONResponse: invalidPageTokenBadRequest(err),
			}, nil
		}
		h.logger.ErrorContext(ctx, "Failed to list catalog item instances", "error", err)
		return server.ListCatalogItemInstances500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}, nil
	}

	h.logger.DebugContext(ctx, "Listed catalog item instances", "count", len(result.CatalogItemInstances))

	// Return HTTP response
	response := server.ListCatalogItemInstances200JSONResponse(v1alpha1.CatalogItemInstanceList{
		Results: result.CatalogItemInstances,
	})
	if result.NextPageToken != nil {
		response.NextPageToken = *result.NextPageToken
	}
	return response, nil
}

func (h *Handler) CreateCatalogItemInstance(ctx context.Context, request server.CreateCatalogItemInstanceRequestObject) (server.CreateCatalogItemInstanceResponseObject, error) {
	h.logger.InfoContext(ctx, "Creating catalog item instance",
		"id", request.Params.Id,
		"catalog_item_id", request.Body.Spec.CatalogItemId,
	)

	// Validate and build service request
	req, err := validateAndBuildCreateCatalogItemInstanceRequest(request)
	if err != nil {
		h.logger.WarnContext(ctx, "Invalid create catalog item instance request", "error", err)
		return server.CreateCatalogItemInstance400JSONResponse(v1alpha1.Error{
			Type:   v1alpha1.INVALIDARGUMENT,
			Status: 400,
			Title:  "Bad Request",
			Detail: stringPtr(err.Error()),
		}), nil
	}

	// Call service layer
	result, err := h.service.CatalogItemInstance().Create(ctx, req)
	if err != nil {
		h.logServiceError(ctx, "Failed to create catalog item instance", err)
		return mapCreateCatalogItemInstanceErrorToHTTP(err), nil
	}

	h.logger.InfoContext(ctx, "Created catalog item instance", "id", request.Params.Id)

	// Return HTTP response
	return server.CreateCatalogItemInstance201JSONResponse(*result), nil
}

func validateAndBuildCreateCatalogItemInstanceRequest(request server.CreateCatalogItemInstanceRequestObject) (*service.CreateCatalogItemInstanceRequest, error) {
	if request.Body.ApiVersion != supportedAPIVersion {
		return nil, ErrInvalidCatalogItemInstanceAPIVersion
	}
	return &service.CreateCatalogItemInstanceRequest{
		ID:          request.Params.Id,
		ApiVersion:  request.Body.ApiVersion,
		DisplayName: request.Body.DisplayName,
		Spec:        request.Body.Spec,
	}, nil
}

func (h *Handler) GetCatalogItemInstance(ctx context.Context, request server.GetCatalogItemInstanceRequestObject) (server.GetCatalogItemInstanceResponseObject, error) {
	h.logger.DebugContext(ctx, "Getting catalog item instance", "id", request.CatalogItemInstanceId)

	// Call service layer
	result, err := h.service.CatalogItemInstance().Get(ctx, request.CatalogItemInstanceId)
	if err != nil {
		h.logServiceError(ctx, "Failed to get catalog item instance", err, "id", request.CatalogItemInstanceId)
		return mapGetCatalogItemInstanceErrorToHTTP(err), nil
	}

	// Return HTTP response
	return server.GetCatalogItemInstance200JSONResponse(*result), nil
}

func (h *Handler) RehydrateCatalogItemInstance(ctx context.Context, request server.RehydrateCatalogItemInstanceRequestObject) (server.RehydrateCatalogItemInstanceResponseObject, error) {
	h.logger.InfoContext(ctx, "Rehydrating catalog item instance", "id", request.CatalogItemInstanceId)

	result, err := h.service.CatalogItemInstance().Rehydrate(ctx, request.CatalogItemInstanceId)
	if err != nil {
		h.logServiceError(ctx, "Failed to rehydrate catalog item instance", err, "id", request.CatalogItemInstanceId)
		return mapRehydrateCatalogItemInstanceErrorToHTTP(err), nil
	}

	h.logger.InfoContext(ctx, "Rehydrated catalog item instance", "id", request.CatalogItemInstanceId)
	return server.RehydrateCatalogItemInstance200JSONResponse(*result), nil
}

func (h *Handler) DeleteCatalogItemInstance(ctx context.Context, request server.DeleteCatalogItemInstanceRequestObject) (server.DeleteCatalogItemInstanceResponseObject, error) {
	h.logger.InfoContext(ctx, "Deleting catalog item instance", "id", request.CatalogItemInstanceId)

	// Call service layer
	err := h.service.CatalogItemInstance().Delete(ctx, request.CatalogItemInstanceId)
	if err != nil {
		h.logServiceError(ctx, "Failed to delete catalog item instance", err, "id", request.CatalogItemInstanceId)
		return mapDeleteCatalogItemInstanceErrorToHTTP(err), nil
	}

	h.logger.InfoContext(ctx, "Deleted catalog item instance", "id", request.CatalogItemInstanceId)

	// Return HTTP 204 No Content response
	return server.DeleteCatalogItemInstance204Response{}, nil
}
