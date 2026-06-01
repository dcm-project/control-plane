package opa

import (
	"errors"
)

// Sentinel errors for policy engine operations
var (
	// ErrInvalidRego indicates that the Rego code is syntactically invalid
	ErrInvalidRego = errors.New("invalid Rego code")

	// ErrEngineInternal indicates an unexpected error within the policy engine
	ErrEngineInternal = errors.New("policy engine internal error")
)
