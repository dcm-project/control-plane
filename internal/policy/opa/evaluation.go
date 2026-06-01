package opa

// EvaluationResult represents the result from OPA evaluation
type EvaluationResult struct {
	Result  map[string]any // The policy decision
	Defined bool           // Whether the policy made a decision
}

// ServiceProviderConstraints represents constraints on which service providers are allowed
type ServiceProviderConstraints struct {
	AllowList []string `json:"allow_list,omitempty"`
	Patterns  []string `json:"patterns,omitempty"`
}

// PolicyDecision represents the expected output from OPA policies
type PolicyDecision struct {
	Rejected                   bool                        `json:"rejected"`
	RejectionReason            string                      `json:"rejection_reason,omitempty"`
	Patch                      map[string]any              `json:"patch,omitempty"`
	Constraints                map[string]any              `json:"constraints,omitempty"`
	ServiceProviderConstraints *ServiceProviderConstraints `json:"service_provider_constraints,omitempty"`
	SelectedProvider           string                      `json:"selected_provider,omitempty"`
}

// ParsePolicyDecision extracts a PolicyDecision from the OPA evaluation result
func ParsePolicyDecision(result map[string]any) *PolicyDecision {
	decision := &PolicyDecision{}

	if rejected, ok := result["rejected"].(bool); ok {
		decision.Rejected = rejected
	}

	if reason, ok := result["rejection_reason"].(string); ok {
		decision.RejectionReason = reason
	}

	if patch, ok := result["patch"].(map[string]any); ok {
		decision.Patch = patch
	}

	if constraints, ok := result["constraints"].(map[string]any); ok {
		decision.Constraints = constraints
	}

	if spc, ok := result["service_provider_constraints"].(map[string]any); ok {
		spConstraints := &ServiceProviderConstraints{}
		if allowList, ok := spc["allow_list"].([]any); ok {
			for _, item := range allowList {
				if s, ok := item.(string); ok {
					spConstraints.AllowList = append(spConstraints.AllowList, s)
				}
			}
		}
		if patterns, ok := spc["patterns"].([]any); ok {
			for _, item := range patterns {
				if s, ok := item.(string); ok {
					spConstraints.Patterns = append(spConstraints.Patterns, s)
				}
			}
		}
		decision.ServiceProviderConstraints = spConstraints
	}

	if provider, ok := result["selected_provider"].(string); ok {
		decision.SelectedProvider = provider
	}

	return decision
}
