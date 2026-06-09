package opa

import (
	"errors"
)

// Sentinel errors for policy engine operations
var (
	// ErrInvalidRego indicates that the Rego code is syntactically invalid
	ErrInvalidRego = errors.New("invalid Rego code")

	// ErrMissingMainRule indicates that the Rego module does not define a main rule.
	ErrMissingMainRule = errors.New("rego code must define a main rule")

	// ErrEngineInternal indicates an unexpected error within the policy engine
	ErrEngineInternal = errors.New("policy engine internal error")
)
