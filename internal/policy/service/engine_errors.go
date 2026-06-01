package service

import (
	"errors"
	"fmt"

	"github.com/dcm-project/control-plane/internal/policy/opa"
)

// handleEngineError maps policy engine errors to ServiceError types
func handleEngineError(err error, operation string) *ServiceError {
	if err == nil {
		return nil
	}

	if errors.Is(err, opa.ErrInvalidRego) {
		return NewInvalidArgumentError(
			"Invalid Rego code",
			fmt.Sprintf("The Rego code contains syntax errors: %v", err),
		)
	}

	if errors.Is(err, opa.ErrEngineInternal) {
		return NewInternalError(
			fmt.Sprintf("Policy engine error during %s", operation),
			"An unexpected error occurred in the policy engine",
			err,
		)
	}

	// Generic engine error
	return NewInternalError(
		fmt.Sprintf("Policy engine error during %s", operation),
		"An unexpected error occurred in the policy engine",
		err,
	)
}
