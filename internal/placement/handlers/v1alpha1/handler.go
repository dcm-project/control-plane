// Package v1alpha1 handlers implements the HTTP request handlers for the placement API.
package v1alpha1

import (
	"context"

	"github.com/dcm-project/control-plane/internal/placement/api/server"
	"github.com/dcm-project/control-plane/internal/placement/logging"
	"github.com/dcm-project/control-plane/internal/placement/service"
)

// Handler implements the generated StrictServerInterface for the Placement API.
type Handler struct {
	placementService *service.PlacementService
}

// NewHandler creates a new Handler with the given placement service.
func NewHandler(placementService *service.PlacementService) *Handler {
	return &Handler{placementService: placementService}
}

// Ensure Handler implements StrictServerInterface
var _ server.StrictServerInterface = (*Handler)(nil)

// ListResources returns a paginated list of resources.
func (h *Handler) ListResources(ctx context.Context, request server.ListResourcesRequestObject) (server.ListResourcesResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("ListResources request received",
		"provider", request.Params.Provider,
		"page_size", request.Params.MaxPageSize,
	)

	result, err := h.placementService.ListResources(
		ctx,
		request.Params.Provider,
		request.Params.MaxPageSize,
		request.Params.PageToken,
	)
	if err != nil {
		logServiceError(ctx, "ListResources failed", err)
		return handleListResourcesError(err), nil
	}

	log.Debug("ListResources completed", "count", len(result.Resources))
	return server.ListResources200JSONResponse{
		Resources:     result.Resources,
		NextPageToken: result.NextPageToken,
	}, nil
}

// CreateResource creates a new resource.
func (h *Handler) CreateResource(ctx context.Context, request server.CreateResourceRequestObject) (server.CreateResourceResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("CreateResource request received",
		"client_id", request.Params.Id,
		"catalog_item_instance_id", request.Body.CatalogItemInstanceId,
	)

	result, err := h.placementService.CreateResource(ctx, request.Body, request.Params.Id)
	if err != nil {
		logServiceError(ctx, "CreateResource failed", err)
		return handleCreateResourceError(err), nil
	}

	log.Info("Resource created", "resource_id", *result.Id)
	return server.CreateResource201JSONResponse(*result), nil
}

// GetResource retrieves a resource by ID.
func (h *Handler) GetResource(ctx context.Context, request server.GetResourceRequestObject) (server.GetResourceResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("GetResource request received", "resource_id", request.ResourceId)

	result, err := h.placementService.GetResource(ctx, request.ResourceId)
	if err != nil {
		logServiceError(ctx, "GetResource failed", err, "resource_id", request.ResourceId)
		return handleGetResourceError(err), nil
	}

	log.Debug("GetResource completed", "resource_id", request.ResourceId)
	return server.GetResource200JSONResponse(*result), nil
}

// DeleteResource deletes a resource by ID.
func (h *Handler) DeleteResource(ctx context.Context, request server.DeleteResourceRequestObject) (server.DeleteResourceResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("DeleteResource request received", "resource_id", request.ResourceId)

	err := h.placementService.DeleteResource(ctx, request.ResourceId)
	if err != nil {
		logServiceError(ctx, "DeleteResource failed", err, "resource_id", request.ResourceId)
		return handleDeleteResourceError(err), nil
	}

	log.Info("Resource deleted", "resource_id", request.ResourceId)
	return server.DeleteResource204Response{}, nil
}

// RehydrateResource re-evaluates and recreates a resource with a new ID.
func (h *Handler) RehydrateResource(ctx context.Context, request server.RehydrateResourceRequestObject) (server.RehydrateResourceResponseObject, error) {
	log := logging.FromContext(ctx)
	log.Debug("RehydrateResource request received",
		"resource_id", request.ResourceId,
		"new_resource_id", request.Body.NewResourceId,
	)

	result, err := h.placementService.RehydrateResource(ctx, request.ResourceId, request.Body.NewResourceId)
	if err != nil {
		logServiceError(ctx, "RehydrateResource failed", err, "resource_id", request.ResourceId)
		return handleRehydrateResourceError(err), nil
	}

	log.Info("Resource rehydrated",
		"old_resource_id", request.ResourceId,
		"new_resource_id", *result.Id,
	)
	return server.RehydrateResource200JSONResponse(*result), nil
}
