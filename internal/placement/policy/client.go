// Package policy provides a client for the policy evaluation service.
package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/dcm-project/control-plane/internal/placement/httputil"
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
	endpoint   string
	httpClient *http.Client
	retryOpts  []backoff.RetryOption
}

type evaluateRequestBody struct {
	ServiceInstance struct {
		Spec map[string]any `json:"spec"`
	} `json:"service_instance"`
}

type evaluateResponseBody struct {
	Status                   string `json:"status"`
	SelectedProvider         string `json:"selected_provider"`
	EvaluatedServiceInstance struct {
		Spec map[string]any `json:"spec"`
	} `json:"evaluated_service_instance"`
}

// NewClient creates a remote policy evaluation client for subsystem tests and
// split deployments. Production monolith wiring uses NewLocalClient instead.
func NewClient(baseURL string, timeout time.Duration) (Client, error) {
	endpoint, err := buildEvaluateURL(baseURL)
	if err != nil {
		return nil, err
	}

	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return &client{
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: timeout},
		retryOpts:  httputil.DefaultRetryOpts(),
	}, nil
}

// Evaluate sends a service instance spec to the policy engine for evaluation
func (c *client) Evaluate(ctx context.Context, req EvaluateRequest) (*EvaluateResponse, error) {
	body := evaluateRequestBody{}
	body.ServiceInstance.Spec = req.Spec

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal policy evaluation request: %w", err)
	}

	operation := func() (*EvaluateResponse, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("failed to create policy evaluation request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("failed to call policy engine: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read policy engine response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			httpErr := &HTTPError{
				StatusCode: resp.StatusCode,
				Body:       string(respBody),
			}
			if httputil.IsPermanentHTTPError(resp.StatusCode) {
				return nil, backoff.Permanent(httpErr)
			}
			return nil, httpErr
		}

		var parsed evaluateResponseBody
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return nil, fmt.Errorf("failed to decode policy evaluation response: %w", err)
		}

		return &EvaluateResponse{
			Status:           parsed.Status,
			SelectedProvider: parsed.SelectedProvider,
			EvaluatedSpec:    parsed.EvaluatedServiceInstance.Spec,
		}, nil
	}

	return backoff.Retry(ctx, operation, c.retryOpts...)
}

func buildEvaluateURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("policy evaluation URL is required")
	}

	apiBase, err := url.JoinPath(strings.TrimRight(baseURL, "/"), "api", "v1alpha1")
	if err != nil {
		return "", fmt.Errorf("failed to build policy evaluation URL: %w", err)
	}
	return apiBase + "/policies:evaluateRequest", nil
}
