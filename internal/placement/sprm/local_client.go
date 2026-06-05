package sprm

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	sprmv1alpha1 "github.com/dcm-project/control-plane/api/sp/v1alpha1/resource_manager"
	spservice "github.com/dcm-project/control-plane/internal/sp/service"
	rmsvc "github.com/dcm-project/control-plane/internal/sp/service/resource_manager"
)

type localClient struct {
	instances *rmsvc.InstanceService
}

// NewLocalClient creates an in-process SPRM client backed by InstanceService.
func NewLocalClient(instances *rmsvc.InstanceService) Client {
	return &localClient{instances: instances}
}

func (c *localClient) CreateResource(ctx context.Context, req CreateResourceRequest) (*CreateResourceResponse, error) {
	body := sprmv1alpha1.ServiceTypeInstance{
		ProviderName: req.ProviderName,
		Spec:         req.Spec,
	}
	queryID := req.ID

	instance, err := c.instances.CreateInstance(ctx, &body, &queryID)
	if err != nil {
		return nil, mapInstanceError(err)
	}

	resp := &CreateResourceResponse{}
	if instance.Id != nil {
		resp.ID = *instance.Id
	}
	if instance.Status != nil {
		resp.Status = *instance.Status
	}
	return resp, nil
}

func (c *localClient) DeleteResource(ctx context.Context, resourceID string) error {
	return c.deleteResource(ctx, resourceID, false)
}

func (c *localClient) DeleteResourceDeferred(ctx context.Context, resourceID string) error {
	return c.deleteResource(ctx, resourceID, true)
}

func (c *localClient) deleteResource(ctx context.Context, resourceID string, deferred bool) error {
	if err := c.instances.DeleteInstance(ctx, resourceID, deferred); err != nil {
		return mapInstanceError(err)
	}
	return nil
}

func mapInstanceError(err error) error {
	var svcErr *spservice.ServiceError
	if !errors.As(err, &svcErr) {
		return fmt.Errorf("sprm service error: %w", err)
	}

	status := http.StatusInternalServerError
	switch svcErr.Code {
	case spservice.ErrCodeValidation:
		status = http.StatusBadRequest
	case spservice.ErrCodeNotFound:
		status = http.StatusNotFound
	case spservice.ErrCodeConflict:
		status = http.StatusConflict
	case spservice.ErrCodeProviderError:
		status = http.StatusUnprocessableEntity
	}

	return &HTTPError{StatusCode: status, Body: svcErr.Message}
}
