package placement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dcm-project/control-plane/internal/placement/service"
	"github.com/dcm-project/control-plane/internal/placement/types"
)

type localClient struct {
	svc    *service.PlacementService
	logger *slog.Logger
}

// NewLocalClient creates an in-process placement client backed by PlacementService.
func NewLocalClient(svc *service.PlacementService, logger *slog.Logger) Client {
	return &localClient{svc: svc, logger: logger.With("component", "placement-local")}
}

func (c *localClient) CreateResource(ctx context.Context, req CreateResourceRequest, id string) (*Resource, error) {
	c.logger.InfoContext(ctx, "Creating resource in placement (in-process)",
		"catalog_item_instance_id", req.CatalogItemInstanceID,
		"resource_id", id,
	)

	body := &types.Resource{
		CatalogItemInstanceId: req.CatalogItemInstanceID,
		Spec:                  req.Spec,
	}
	var queryID *string
	if id != "" {
		queryID = &id
	}

	result, err := c.svc.CreateResource(ctx, body, queryID)
	if err != nil {
		return nil, mapPlacementServiceError(err)
	}
	return mapAPIResource(result), nil
}

func (c *localClient) DeleteResource(ctx context.Context, resourceID string) error {
	c.logger.InfoContext(ctx, "Deleting resource in placement (in-process)", "resource_id", resourceID)
	if err := c.svc.DeleteResource(ctx, resourceID); err != nil {
		return mapPlacementServiceError(err)
	}
	return nil
}

func (c *localClient) RehydrateResource(ctx context.Context, resourceID string, newResourceID string) (*Resource, error) {
	c.logger.InfoContext(ctx, "Rehydrating resource in placement (in-process)",
		"resource_id", resourceID,
		"new_resource_id", newResourceID,
	)
	result, err := c.svc.RehydrateResource(ctx, resourceID, newResourceID)
	if err != nil {
		return nil, mapPlacementServiceError(err)
	}
	return mapAPIResource(result), nil
}

func mapAPIResource(r *types.Resource) *Resource {
	res := &Resource{Spec: r.Spec}
	if r.Id != nil {
		res.ID = *r.Id
	}
	if r.Path != nil {
		res.Path = *r.Path
	}
	return res
}

func mapPlacementServiceError(err error) error {
	var svcErr *service.ServiceError
	if !errors.As(err, &svcErr) {
		return fmt.Errorf("placement service error: %w", err)
	}

	status := http.StatusInternalServerError
	switch svcErr.Code {
	case service.ErrCodeValidation:
		status = http.StatusBadRequest
	case service.ErrCodePolicyRejected:
		status = http.StatusNotAcceptable
	case service.ErrCodeConflict, service.ErrCodePolicyConflict:
		status = http.StatusConflict
	case service.ErrCodeProviderError:
		status = http.StatusUnprocessableEntity
	case service.ErrCodeNotFound:
		status = http.StatusNotFound
	case service.ErrCodePolicyError, service.ErrCodePolicyInternalError:
		status = http.StatusFailedDependency
	}

	return &PlacementError{StatusCode: status, Body: svcErr.Message}
}
