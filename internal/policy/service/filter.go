package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dcm-project/control-plane/internal/policy/store"
)

var (
	// Regex patterns for CEL filter parsing
	policyTypePattern = regexp.MustCompile(`policy_type\s*=\s*'(GLOBAL|USER)'`)
	enabledPattern    = regexp.MustCompile(`enabled\s*=\s*(true|false)`)
)

// parseFilter parses a CEL filter expression into a PolicyFilter.
// Supports filtering by policy_type and enabled fields.
//
// Supported expressions:
//   - policy_type='GLOBAL'
//   - policy_type='USER'
//   - enabled=true
//   - enabled=false
//   - policy_type='GLOBAL' AND enabled=true
//   - enabled=true AND policy_type='USER'
//
// Returns an error for invalid filter expressions.
func parseFilter(filterExpr string) (*store.PolicyFilter, error) {
	if filterExpr == "" {
		return nil, nil
	}

	filter := &store.PolicyFilter{}

	// Parse policy_type filter
	if matches := policyTypePattern.FindStringSubmatch(filterExpr); len(matches) > 1 {
		policyType := matches[1]
		filter.PolicyType = &policyType
	}

	// Parse enabled filter
	if matches := enabledPattern.FindStringSubmatch(filterExpr); len(matches) > 1 {
		enabledStr := matches[1]
		enabled := enabledStr == "true"
		filter.Enabled = &enabled
	}

	// Validate that we parsed something
	if filter.PolicyType == nil && filter.Enabled == nil {
		return nil, NewInvalidArgumentError(
			"Invalid filter expression",
			fmt.Sprintf("Filter expression '%s' does not contain valid conditions. Supported fields: policy_type, enabled", filterExpr),
		)
	}

	// Validate AND operator usage if present
	if strings.Contains(filterExpr, " AND ") {
		// If AND is present, both conditions should be present
		parts := strings.Split(filterExpr, " AND ")
		if len(parts) != 2 {
			return nil, NewInvalidArgumentError(
				"Invalid filter expression",
				"Multiple AND operators are not supported",
			)
		}
	}

	return filter, nil
}
