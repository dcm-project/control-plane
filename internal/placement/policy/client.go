// Package policy provides a client for the policy evaluation service.
package policy

import (
	"context"
	"fmt"
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

// Client defines the interface for policy evaluation.
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
