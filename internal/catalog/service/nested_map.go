package service

import (
	"fmt"
	"strings"
)

// stripSpecPrefix removes the "spec." prefix from a path if present
func stripSpecPrefix(path string) string {
	return strings.TrimPrefix(path, "spec.")
}

// setNestedValue sets a value in a nested map at the given dot-notation path.
// Creates intermediate maps as needed.
func setNestedValue(m map[string]any, path string, value any) error {
	path = stripSpecPrefix(path)
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	parts := strings.Split(path, ".")

	current := m
	for i := 0; i < len(parts)-1; i++ {
		key := parts[i]
		next, exists := current[key]
		if !exists {
			// Create intermediate map
			newMap := make(map[string]any)
			current[key] = newMap
			current = newMap
			continue
		}
		nextMap, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("path segment %q is not a map", strings.Join(parts[:i+1], "."))
		}
		current = nextMap
	}

	current[parts[len(parts)-1]] = value
	return nil
}

// getNestedValue retrieves a value from a nested map at the given dot-notation path.
func getNestedValue(m map[string]any, path string) (any, error) {
	path = stripSpecPrefix(path)
	if path == "" {
		return nil, fmt.Errorf("path cannot be empty")
	}
	parts := strings.Split(path, ".")

	current := m
	for i := 0; i < len(parts)-1; i++ {
		key := parts[i]
		next, exists := current[key]
		if !exists {
			return nil, fmt.Errorf("path segment %q not found", strings.Join(parts[:i+1], "."))
		}
		nextMap, ok := next.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("path segment %q is not a map", strings.Join(parts[:i+1], "."))
		}
		current = nextMap
	}

	val, exists := current[parts[len(parts)-1]]
	if !exists {
		return nil, fmt.Errorf("path %q not found", path)
	}
	return val, nil
}
