package service

// MatchesLabelSelector checks if request labels match the policy label selector.
// Uses AND semantics: all policy selector labels must match request labels.
// Empty policy selector matches all requests.
func MatchesLabelSelector(policySelector map[string]string, requestLabels map[string]string) bool {
	// Empty selector matches all requests
	if len(policySelector) == 0 {
		return true
	}

	// All policy selector labels must be present and match in request labels
	for key, value := range policySelector {
		requestValue, exists := requestLabels[key]
		if !exists || requestValue != value {
			return false
		}
	}

	return true
}
