// Package placement provides a client for the Placement Manager service.
package placement

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	pmv1alpha1 "github.com/dcm-project/control-plane/api/placement/v1alpha1"
	pmclient "github.com/dcm-project/control-plane/pkg/placement/client"
)

// PlacementError represents a structured error from the Placement Manager,
// preserving the HTTP status code for upstream handling.
type PlacementError struct {
	StatusCode int
	Body       string
}

func (e *PlacementError) Error() string {
	return fmt.Sprintf("placement manager returned status %d: %s", e.StatusCode, e.Body)
}

// CreateResourceRequest is the request body for creating a resource in the Placement Manager
type CreateResourceRequest struct {
	CatalogItemInstanceID string         `json:"catalog_item_instance_id"`
	Spec                  map[string]any `json:"spec"`
}

// Resource is the response from the Placement Manager
type Resource struct {
	ID   string         `json:"id"`
	Path string         `json:"path"`
	Spec map[string]any `json:"spec"`
}

// Client defines the interface for interacting with the Placement Manager
type Client interface {
	CreateResource(ctx context.Context, req CreateResourceRequest, id string) (*Resource, error)
	DeleteResource(ctx context.Context, resourceID string) error
	RehydrateResource(ctx context.Context, resourceID string, newResourceID string) (*Resource, error)
}

type client struct {
	pm     *pmclient.ClientWithResponses
	logger *slog.Logger
}

// NewClient creates a new Placement Manager client.
func NewClient(baseURL string, timeout time.Duration, logger *slog.Logger, opts ...pmclient.ClientOption) (Client, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	serverURL, err := apiBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: timeout}
	opts = append([]pmclient.ClientOption{pmclient.WithHTTPClient(httpClient)}, opts...)

	pm, err := pmclient.NewClientWithResponses(serverURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create placement manager client: %w", err)
	}
	logger.Info("Placement manager client created", "url", serverURL, "timeout", timeout)
	return &client{pm: pm, logger: logger.With("component", "placement")}, nil
}

func apiBaseURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("placement manager URL is required")
	}
	return url.JoinPath(strings.TrimRight(baseURL, "/"), "api", "v1alpha1")
}

// CreateResource creates a resource in the Placement Manager
func (c *client) CreateResource(ctx context.Context, req CreateResourceRequest, id string) (*Resource, error) {
	c.logger.InfoContext(ctx, "Creating resource in placement manager",
		"catalog_item_instance_id", req.CatalogItemInstanceID,
		"resource_id", id,
	)

	params := &pmv1alpha1.CreateResourceParams{}
	if id != "" {
		params.Id = &id
	}

	body := pmv1alpha1.Resource{
		CatalogItemInstanceId: req.CatalogItemInstanceID,
		Spec:                  req.Spec,
	}

	resp, err := c.pm.CreateResourceWithResponse(ctx, params, body)
	if err != nil {
		c.logger.ErrorContext(ctx, "Placement manager create resource call failed",
			"resource_id", id,
			"error", err,
		)
		return nil, fmt.Errorf("failed to call placement manager: %w", err)
	}

	if resp.JSON201 == nil {
		c.logger.ErrorContext(ctx, "Placement manager returned unexpected status",
			"resource_id", id,
			"status", resp.StatusCode(),
		)
		return nil, &PlacementError{StatusCode: resp.StatusCode(), Body: string(resp.Body)}
	}

	c.logger.InfoContext(ctx, "Resource created in placement manager", "resource_id", id)
	return mapResource(resp.JSON201), nil
}

// DeleteResource deletes a resource from the Placement Manager
func (c *client) DeleteResource(ctx context.Context, resourceID string) error {
	c.logger.InfoContext(ctx, "Deleting resource from placement manager", "resource_id", resourceID)

	resp, err := c.pm.DeleteResourceWithResponse(ctx, resourceID)
	if err != nil {
		c.logger.ErrorContext(ctx, "Placement manager delete resource call failed",
			"resource_id", resourceID,
			"error", err,
		)
		return fmt.Errorf("failed to call placement manager: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		c.logger.ErrorContext(ctx, "Placement manager delete returned unexpected status",
			"resource_id", resourceID,
			"status", resp.StatusCode(),
		)
		return fmt.Errorf("placement manager returned status %d: %s", resp.StatusCode(), string(resp.Body))
	}

	c.logger.InfoContext(ctx, "Resource deleted from placement manager", "resource_id", resourceID)
	return nil
}

// RehydrateResource rehydrates a resource in the Placement Manager
func (c *client) RehydrateResource(ctx context.Context, resourceID string, newResourceID string) (*Resource, error) {
	c.logger.InfoContext(ctx, "Rehydrating resource in placement manager",
		"resource_id", resourceID,
		"new_resource_id", newResourceID,
	)

	body := pmv1alpha1.RehydrateResourceJSONRequestBody{
		NewResourceId: newResourceID,
	}

	resp, err := c.pm.RehydrateResourceWithResponse(ctx, resourceID, body)
	if err != nil {
		c.logger.ErrorContext(ctx, "Placement manager rehydrate resource call failed",
			"resource_id", resourceID,
			"error", err,
		)
		return nil, fmt.Errorf("failed to call placement manager: %w", err)
	}

	if resp.JSON200 == nil {
		c.logger.ErrorContext(ctx, "Placement manager returned unexpected status",
			"resource_id", resourceID,
			"status", resp.StatusCode(),
		)
		return nil, &PlacementError{StatusCode: resp.StatusCode(), Body: string(resp.Body)}
	}

	c.logger.InfoContext(ctx, "Resource rehydrated in placement manager",
		"resource_id", resourceID,
		"new_resource_id", newResourceID,
	)
	return mapResource(resp.JSON200), nil
}

func mapResource(r *pmv1alpha1.Resource) *Resource {
	res := &Resource{
		Spec: r.Spec,
	}
	if r.Id != nil {
		res.ID = *r.Id
	}
	if r.Path != nil {
		res.Path = *r.Path
	}
	return res
}
