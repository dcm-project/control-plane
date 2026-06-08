// Package placement provides a client for the Placement Manager service.
package placement

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
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
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

type resourceResponseBody struct {
	ID                    *string        `json:"id,omitempty"`
	Path                  *string        `json:"path,omitempty"`
	CatalogItemInstanceID string         `json:"catalog_item_instance_id,omitempty"`
	Spec                  map[string]any `json:"spec,omitempty"`
}

type rehydrateRequestBody struct {
	NewResourceID string `json:"new_resource_id"`
}

// NewClient creates a remote placement client for subsystem tests and split
// deployments. Production monolith wiring uses NewLocalClient instead.
func NewClient(baseURL string, timeout time.Duration, logger *slog.Logger) (Client, error) {
	apiBase, err := apiBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	logger.Info("Placement manager client created", "url", apiBase, "timeout", timeout)
	return &client{
		baseURL:    apiBase,
		httpClient: &http.Client{Timeout: timeout},
		logger:     logger.With("component", "placement"),
	}, nil
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

	endpoint := c.baseURL + "/resources"
	if id != "" {
		endpoint = endpoint + "?id=" + url.QueryEscape(id)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal placement create request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create placement request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.ErrorContext(ctx, "Placement manager create resource call failed", "resource_id", id, "error", err)
		return nil, fmt.Errorf("failed to call placement manager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read placement create response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		c.logger.ErrorContext(ctx, "Placement manager returned unexpected status",
			"resource_id", id,
			"status", resp.StatusCode,
		)
		return nil, &PlacementError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var parsed resourceResponseBody
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode placement create response: %w", err)
	}

	c.logger.InfoContext(ctx, "Resource created in placement manager", "resource_id", id)
	return mapResourceResponse(&parsed), nil
}

// DeleteResource deletes a resource from the Placement Manager
func (c *client) DeleteResource(ctx context.Context, resourceID string) error {
	c.logger.InfoContext(ctx, "Deleting resource from placement manager", "resource_id", resourceID)

	endpoint, err := url.JoinPath(c.baseURL, "resources", resourceID)
	if err != nil {
		return fmt.Errorf("failed to build placement delete URL: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create placement delete request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.ErrorContext(ctx, "Placement manager delete resource call failed",
			"resource_id", resourceID,
			"error", err,
		)
		return fmt.Errorf("failed to call placement manager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read placement delete response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.ErrorContext(ctx, "Placement manager delete returned unexpected status",
			"resource_id", resourceID,
			"status", resp.StatusCode,
		)
		return fmt.Errorf("placement manager returned status %d: %s", resp.StatusCode, string(respBody))
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

	endpoint := c.baseURL + "/resources/" + url.PathEscape(resourceID) + ":rehydrate"
	payload, err := json.Marshal(rehydrateRequestBody{NewResourceID: newResourceID})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal placement rehydrate request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create placement rehydrate request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.ErrorContext(ctx, "Placement manager rehydrate resource call failed",
			"resource_id", resourceID,
			"error", err,
		)
		return nil, fmt.Errorf("failed to call placement manager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read placement rehydrate response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.ErrorContext(ctx, "Placement manager returned unexpected status",
			"resource_id", resourceID,
			"status", resp.StatusCode,
		)
		return nil, &PlacementError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var parsed resourceResponseBody
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode placement rehydrate response: %w", err)
	}

	c.logger.InfoContext(ctx, "Resource rehydrated in placement manager",
		"resource_id", resourceID,
		"new_resource_id", newResourceID,
	)
	return mapResourceResponse(&parsed), nil
}

func mapResourceResponse(r *resourceResponseBody) *Resource {
	res := &Resource{Spec: r.Spec}
	if r.ID != nil {
		res.ID = *r.ID
	}
	if r.Path != nil {
		res.Path = *r.Path
	}
	return res
}
