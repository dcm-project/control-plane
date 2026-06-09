// Package policy provides types and an adapter boundary for policy evaluation.
package policy

import (
	"context"
	"fmt"
)

// EvaluateRequest is the input for policy evaluation.
type EvaluateRequest struct {
	Spec map[string]any `json:"spec"`
}

// EvaluateResponse is the result of policy evaluation.
type EvaluateResponse struct {
	Status           string         `json:"status"`
	SelectedProvider string         `json:"selected_provider"`
	EvaluatedSpec    map[string]any `json:"evaluated_spec"`
}

// Client is the port PlacementService uses to evaluate policies.
type Client interface {
	Evaluate(ctx context.Context, req EvaluateRequest) (*EvaluateResponse, error)
}

// HTTPError carries a status code and message from the evaluation adapter.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("policy evaluation returned status %d: %s", e.StatusCode, e.Body)
}
