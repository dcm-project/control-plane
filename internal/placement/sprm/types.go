// Package sprm provides types and an adapter boundary for SP resource instances.
package sprm

import (
	"context"
	"fmt"
)

// CreateResourceRequest is the input for creating a service type instance.
type CreateResourceRequest struct {
	ID           string         `json:"id"`
	Spec         map[string]any `json:"spec"`
	ProviderName string         `json:"provider_name"`
}

// CreateResourceResponse is the result of creating a service type instance.
type CreateResourceResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Client is the port PlacementService uses to manage SP resource instances.
type Client interface {
	CreateResource(ctx context.Context, req CreateResourceRequest) (*CreateResourceResponse, error)
	DeleteResource(ctx context.Context, resourceId string) error
	DeleteResourceDeferred(ctx context.Context, resourceId string) error
}

// HTTPError carries a status code and message from the SPRM adapter.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("sprm returned status %d: %s", e.StatusCode, e.Body)
}
