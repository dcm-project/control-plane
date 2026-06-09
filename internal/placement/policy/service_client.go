package policy

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	policyservice "github.com/dcm-project/control-plane/internal/policy/service"
)

type serviceClient struct {
	eval policyservice.EvaluationService
}

// NewServiceClient adapts EvaluationService for in-process use by PlacementService.
func NewServiceClient(eval policyservice.EvaluationService) Client {
	return &serviceClient{eval: eval}
}

func (c *serviceClient) Evaluate(ctx context.Context, req EvaluateRequest) (*EvaluateResponse, error) {
	response, err := c.eval.EvaluateRequest(ctx, &policyservice.EvaluationRequest{
		ServiceInstance: req.Spec,
	})
	if err != nil {
		return nil, mapEvaluationError(err)
	}

	status := string(response.Status)
	return &EvaluateResponse{
		Status:           status,
		SelectedProvider: response.SelectedProvider,
		EvaluatedSpec:    response.EvaluatedServiceInstance,
	}, nil
}

func mapEvaluationError(err error) error {
	var svcErr *policyservice.ServiceError
	if !errors.As(err, &svcErr) {
		return fmt.Errorf("policy evaluation error: %w", err)
	}

	status := http.StatusInternalServerError
	switch svcErr.Type {
	case policyservice.ErrorTypeInvalidArgument:
		status = http.StatusBadRequest
	case policyservice.ErrorTypeRejected:
		status = http.StatusNotAcceptable
	case policyservice.ErrorTypePolicyConflict:
		status = http.StatusConflict
	}

	return &HTTPError{StatusCode: status, Body: svcErr.Message}
}
