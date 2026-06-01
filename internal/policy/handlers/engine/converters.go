package engine

import (
	"fmt"

	engineapi "github.com/dcm-project/control-plane/api/policy/v1alpha1/engine"
	engineserver "github.com/dcm-project/control-plane/internal/policy/api/engine"
	"github.com/dcm-project/control-plane/internal/policy/service"
)

func toServiceEvaluationRequest(request engineserver.EvaluateRequestRequestObject) (*service.EvaluationRequest, error) {
	requestLabels, err := extractRequestLabels(request.Body.ServiceInstance.Spec)
	if err != nil {
		return nil, err
	}
	return &service.EvaluationRequest{
		ServiceInstance: request.Body.ServiceInstance.Spec,
		RequestLabels:   requestLabels,
	}, nil
}

func toEngineEvaluationResponse(response *service.EvaluationResponse) engineapi.EvaluateResponse {
	return engineapi.EvaluateResponse{
		EvaluatedServiceInstance: engineapi.ServiceInstance{
			Spec: response.EvaluatedServiceInstance,
		},
		SelectedProvider: response.SelectedProvider,
		Status:           engineapi.EvaluateResponseStatus(response.Status),
	}
}

// extractRequestLabels extracts labels from spec.metadata.labels
func extractRequestLabels(spec map[string]any) (map[string]string, error) {
	serviceType, ok := spec["service_type"].(string)
	if !ok {
		return nil, fmt.Errorf("service type is required")
	}
	result := make(map[string]string)
	result["service_type"] = serviceType

	if metadata, ok := spec["metadata"].(map[string]any); ok {
		if labels, ok := metadata["labels"].(map[string]any); ok {
			for k, v := range labels {
				if strVal, ok := v.(string); ok {
					result[k] = strVal
				}
			}
		}
	}

	return result, nil
}
