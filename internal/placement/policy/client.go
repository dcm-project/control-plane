// Package policy provides a client for the policy evaluation service.
package policy

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/dcm-project/control-plane/internal/placement/httputil"
	enginev1alpha1 "github.com/dcm-project/policy-manager/api/v1alpha1/engine"
	"github.com/dcm-project/policy-manager/pkg/engineclient"
)

// EvaluateRequest is the request body for policy evaluation
type EvaluateRequest struct {
	Spec map[string]any `json:"spec"`
}

// EvaluateResponse is the response from policy evaluation
type EvaluateResponse struct {
	Status           string         `json:"status"`
	SelectedProvider string         `json:"selected_provider"`
	EvaluatedSpec    map[string]any `json:"evaluated_spec"`
}

// Client defines the interface for interacting with the policy engine
type Client interface {
	Evaluate(ctx context.Context, req EvaluateRequest) (*EvaluateResponse, error)
}

// HTTPError represents an HTTP error from the policy engine
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("policy engine returned status %d: %s", e.StatusCode, e.Body)
}

type client struct {
	engine    *engineclient.ClientWithResponses
	retryOpts []backoff.RetryOption
}

// NewClient creates a new policy engine client
func NewClient(baseURL string, timeout time.Duration, opts ...engineclient.ClientOption) (Client, error) {
	httpClient := &http.Client{Timeout: timeout}
	opts = append([]engineclient.ClientOption{engineclient.WithHTTPClient(httpClient)}, opts...)

	engine, err := engineclient.NewClientWithResponses(baseURL+"/api/v1alpha1", opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create policy engine client: %w", err)
	}

	return &client{
		engine:    engine,
		retryOpts: httputil.DefaultRetryOpts(),
	}, nil
}

// Evaluate sends a service instance spec to the policy engine for evaluation
func (c *client) Evaluate(ctx context.Context, req EvaluateRequest) (*EvaluateResponse, error) {
	body := enginev1alpha1.EvaluateRequest{
		ServiceInstance: enginev1alpha1.ServiceInstance{
			Spec: req.Spec,
		},
	}

	operation := func() (*EvaluateResponse, error) {
		resp, err := c.engine.EvaluateRequestWithResponse(ctx, body)
		if err != nil {
			return nil, fmt.Errorf("failed to call policy engine: %w", err)
		}

		if resp.JSON200 == nil {
			httpErr := &HTTPError{
				StatusCode: resp.StatusCode(),
				Body:       string(resp.Body),
			}
			if httputil.IsPermanentHTTPError(resp.StatusCode()) {
				return nil, backoff.Permanent(httpErr)
			}
			return nil, httpErr
		}

		return mapEvaluateResponse(resp.JSON200), nil
	}

	return backoff.Retry(ctx, operation, c.retryOpts...)
}

func mapEvaluateResponse(r *enginev1alpha1.EvaluateResponse) *EvaluateResponse {
	return &EvaluateResponse{
		Status:           string(r.Status),
		SelectedProvider: r.SelectedProvider,
		EvaluatedSpec:    r.EvaluatedServiceInstance.Spec,
	}
}
