package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/brunoga/deep/v4"
	"github.com/dcm-project/control-plane/internal/policy/logging"
	"github.com/dcm-project/control-plane/internal/policy/opa"
	"github.com/dcm-project/control-plane/internal/policy/store"
	"github.com/dcm-project/control-plane/internal/policy/store/model"
)

// EvaluationStatus represents the status of the evaluation
type EvaluationStatus string

const (
	EvaluationStatusApproved EvaluationStatus = "APPROVED"
	EvaluationStatusModified EvaluationStatus = "MODIFIED"
)

// EvaluationService defines the interface for policy evaluation
type EvaluationService interface {
	EvaluateRequest(ctx context.Context, req *EvaluationRequest) (*EvaluationResponse, error)
}

// EvaluationRequest represents a request for policy evaluation
type EvaluationRequest struct {
	ServiceInstance map[string]any
	RequestLabels   map[string]string
}

// EvaluationResponse represents the response from policy evaluation
type EvaluationResponse struct {
	EvaluatedServiceInstance map[string]any
	SelectedProvider         string
	Status                   EvaluationStatus
}

// evaluationService implements EvaluationService
type evaluationService struct {
	policyStore store.Policy
	engine      opa.Engine
}

// NewEvaluationService creates a new evaluation service
func NewEvaluationService(policyStore store.Policy, engine opa.Engine) EvaluationService {
	return &evaluationService{
		policyStore: policyStore,
		engine:      engine,
	}
}

// EvaluateRequest evaluates a service instance request against all applicable policies
func (s *evaluationService) EvaluateRequest(ctx context.Context, req *EvaluationRequest) (*EvaluationResponse, error) {
	log := logging.FromContext(ctx)
	log.Debug("Starting policy evaluation", "label_count", len(req.RequestLabels))

	// Initialize the current service instance spec (we'll modify this as we evaluate policies)
	currentSpec, err := deep.Copy(req.ServiceInstance)
	if err != nil {
		return nil, NewInternalError("Failed to make a deep copy of the service instance spec", err.Error(), err)
	}

	// Initialize constraint context
	constraintCtx := NewConstraintContext()

	// Track selected provider across policies (starts unknown)
	selectedProvider := ""

	// Paginate over all enabled policies, ordered by policy_type ASC, priority ASC
	var pageToken *string
	policiesEvaluated := 0
	policiesSkipped := 0
	for {
		policyListResult, err := s.policyStore.List(ctx, &store.PolicyListOptions{
			Filter: &store.PolicyFilter{
				Enabled: boolPtr(true),
			},
			PageSize:  1000,
			PageToken: pageToken,
		})
		if err != nil {
			log.Error("Failed to retrieve policies for evaluation", "error", err)
			return nil, NewInternalError("Failed to retrieve policies", err.Error(), err)
		}

		// Evaluate each policy on this page sequentially
		for _, policy := range policyListResult.Policies {
			// Filter by label selector
			if !MatchesLabelSelector(policy.LabelSelector, req.RequestLabels) {
				policiesSkipped++
				continue
			}

			log.Debug("Evaluating policy", "policy_id", policy.ID, "policy_type", policy.PolicyType, "priority", policy.Priority)

			currentSpec, selectedProvider, err = s.evaluatePolicy(ctx, &policy, currentSpec, selectedProvider, constraintCtx)
			if err != nil {
				log.Warn("Policy evaluation failed", "policy_id", policy.ID, "error", err)
				return nil, err
			}
			policiesEvaluated++
		}

		if policyListResult.NextPageToken == "" {
			break
		}
		pageToken = &policyListResult.NextPageToken
	}

	// Determine status
	status := EvaluationStatusApproved
	if !deep.Equal(req.ServiceInstance, currentSpec) {
		status = EvaluationStatusModified
	}

	log.Info("Policy evaluation completed",
		"status", status,
		"policies_evaluated", policiesEvaluated,
		"policies_skipped", policiesSkipped,
		"selected_provider", selectedProvider,
	)

	return &EvaluationResponse{
		EvaluatedServiceInstance: currentSpec,
		SelectedProvider:         selectedProvider,
		Status:                   status,
	}, nil
}

func (s *evaluationService) evaluatePolicy(
	ctx context.Context,
	policy *model.Policy,
	currentSpec map[string]any,
	selectedProvider string,
	constraintCtx *ConstraintContext,
) (map[string]any, string, error) {
	log := logging.FromContext(ctx)
	// 1. Build OPA input with constraints and SP constraints
	opaInput := map[string]any{
		"spec":     currentSpec,
		"provider": selectedProvider,
	}
	if constraints := constraintCtx.GetConstraintsMap(); constraints != nil {
		opaInput["constraints"] = constraints
	}
	if spConstraints := constraintCtx.GetSPConstraintsMap(); spConstraints != nil {
		opaInput["service_provider_constraints"] = spConstraints
	}

	// 2. Evaluate the policy using the embedded engine
	evalResult, err := s.engine.EvaluatePolicy(ctx, policy.ID, opaInput)
	if err != nil {
		return nil, "", NewInternalError(
			fmt.Sprintf("Failed to evaluate policy '%s'", policy.ID),
			err.Error(),
			err,
		)
	}

	// Skip if policy is undefined
	if !evalResult.Defined {
		log.Debug("Policy returned undefined result, skipping", "policy_id", policy.ID)
		return currentSpec, selectedProvider, nil
	}

	// Parse the policy decision
	decision := opa.ParsePolicyDecision(evalResult.Result)

	// 3. Check for rejection
	if decision.Rejected {
		log.Info("Policy rejected request", "policy_id", policy.ID, "reason", decision.RejectionReason)
		return nil, "", NewPolicyRejectedError(policy.ID, decision.RejectionReason)
	}

	// 4. Validate and merge constraints — new constraints must not loosen existing ones
	if decision.Constraints != nil {
		if err := constraintCtx.MergeConstraints(decision.Constraints, policy.ID); err != nil {
			var conflictErr *ConstraintConflictError
			if errors.As(err, &conflictErr) {
				return nil, "", NewConstraintConflictError(
					policy.ID, conflictErr.FieldPath, conflictErr.SetByPolicy, conflictErr.Reason,
				)
			}
			return nil, "", NewConstraintConflictError(policy.ID, "", "", err.Error())
		}
	}

	// 5. Merge service provider constraints
	if err := constraintCtx.MergeSPConstraints(decision.ServiceProviderConstraints, policy.ID); err != nil {
		return nil, "", NewServiceProviderConstraintError(policy.ID, err.Error())
	}

	// 6. Validate patch against accumulated constraints
	if decision.Patch != nil {
		violations := constraintCtx.ValidatePatch(decision.Patch)
		if len(violations) > 0 {
			return nil, "", NewConstraintViolationError(policy.ID, violations)
		}

		// 7. Apply patch — deep merge into currentSpec (RFC 7396 JSON Merge Patch semantics)
		currentSpec, err = mergePatch(currentSpec, decision.Patch)
		if err != nil {
			return nil, "", NewInternalError("Failed to merge patch into current spec", err.Error(), err)
		}
		log.Debug("Policy patch applied", "policy_id", policy.ID)
	}

	// 8. Validate service provider against SP constraints
	if decision.SelectedProvider != "" {
		if err := constraintCtx.ValidateServiceProvider(decision.SelectedProvider); err != nil {
			return nil, "", NewServiceProviderConstraintError(policy.ID, err.Error())
		}
		log.Debug("Policy selected provider", "policy_id", policy.ID, "provider", decision.SelectedProvider)
		selectedProvider = decision.SelectedProvider
	}

	return currentSpec, selectedProvider, nil
}

// mergePatch performs a recursive JSON Merge Patch (RFC 7396) of patch into base.
// Fields in patch override fields in base. Null values in patch remove fields from base.
// Fields not mentioned in patch are preserved from base.
func mergePatch(base, patch map[string]any) (map[string]any, error) {
	result, err := deep.Copy(base)
	if err != nil {
		return nil, err
	}

	for key, patchValue := range patch {
		if patchValue == nil {
			// null means remove the field
			delete(result, key)
			continue
		}

		patchMap, patchIsMap := patchValue.(map[string]any)
		baseValue, baseExists := result[key]
		baseMap, baseIsMap := baseValue.(map[string]any)

		if patchIsMap && baseExists && baseIsMap {
			// Both are maps — recurse
			result[key], err = mergePatch(baseMap, patchMap)
			if err != nil {
				return nil, err
			}
		} else {
			// Patch value overrides base
			result[key] = patchValue
		}
	}

	return result, nil
}

// boolPtr returns a pointer to a bool value
func boolPtr(b bool) *bool {
	return &b
}
