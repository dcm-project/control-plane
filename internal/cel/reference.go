// Package cel provides restricted ${resource.output} reference parsing and apply-time binding.
package cel

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrInvalidReference indicates a string is not a valid restricted CEL reference.
var ErrInvalidReference = errors.New("invalid CEL expression: must match ${resourceName.outputField}")

var referencePattern = regexp.MustCompile(`^\$\{([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*(?:\[\d+\])?(?:\.[a-zA-Z_][a-zA-Z0-9_]*(?:\[\d+\])?)*)\}$`)

// Reference is a parsed ${resourceName.outputField} expression.
type Reference struct {
	ResourceName string
	OutputField  string
}

// ParseReference parses a restricted CEL reference string.
// Returns isReference=false when value is a plain string with no CEL syntax.
func ParseReference(value string) (Reference, bool, error) {
	if !strings.Contains(value, "${") {
		return Reference{}, false, nil
	}
	matches := referencePattern.FindStringSubmatch(value)
	if matches == nil {
		return Reference{}, true, fmt.Errorf("%w: %q", ErrInvalidReference, value)
	}
	return Reference{
		ResourceName: matches[1],
		OutputField:  matches[2],
	}, true, nil
}
