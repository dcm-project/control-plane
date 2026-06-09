// Package sprm provides a client for the Service Provider Resource Manager.
package sprm

import (
	"context"
	"fmt"
)

// CreateResourceRequest is the request body for creating a resource in SPRM
type CreateResourceRequest struct {
	ID           string         `json:"id"`
	Spec         map[string]any `json:"spec"`
	ProviderName string         `json:"provider_name"`
}

// CreateResourceResponse is the response from creating a resource
type CreateResourceResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Client defines the interface for interacting with the Service Provider Resource Manager
type Client interface {
	CreateResource(ctx context.Context, req CreateResourceRequest) (*CreateResourceResponse, error)
	DeleteResource(ctx context.Context, resourceId string) error
	DeleteResourceDeferred(ctx context.Context, resourceId string) error
}

// HTTPError represents an error from SPRM with an HTTP-style status code.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("sprm returned status %d: %s", e.StatusCode, e.Body)
}
