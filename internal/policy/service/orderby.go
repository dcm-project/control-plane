package service

import (
	"fmt"
	"strings"
)

const (
	// DefaultOrderBy is the default ordering for policy listings
	DefaultOrderBy = "policy_type ASC, priority ASC, id ASC"
)

// Supported order by fields
var supportedOrderByFields = map[string]bool{
	"priority":     true,
	"display_name": true,
	"create_time":  true,
}

// parseOrderBy parses an order_by parameter into GORM format.
// Supports single and multiple field ordering with asc/desc directions.
//
// Supported fields: priority, display_name, create_time
//
// Examples:
//   - "priority asc" → "priority ASC"
//   - "display_name desc" → "display_name DESC"
//   - "create_time desc,priority asc" → "create_time DESC, priority ASC"
//
// If orderBy is empty, returns the default ordering.
//
// Returns an error for invalid fields or directions.
func parseOrderBy(orderBy string) (string, error) {
	if orderBy == "" {
		return DefaultOrderBy, nil
	}

	// Split by comma for multiple fields
	parts := strings.Split(orderBy, ",")
	var gormParts []string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Split by space to get field and direction
		tokens := strings.Fields(part)
		if len(tokens) == 0 {
			continue
		}

		field := tokens[0]
		direction := "ASC" // Default direction

		// Validate field
		if !supportedOrderByFields[field] {
			return "", NewInvalidArgumentError(
				"Invalid order_by field",
				fmt.Sprintf("Field '%s' is not supported for ordering. Supported fields: priority, display_name, create_time", field),
			)
		}

		// Parse direction if provided
		if len(tokens) > 1 {
			dir := strings.ToUpper(tokens[1])
			if dir != "ASC" && dir != "DESC" {
				return "", NewInvalidArgumentError(
					"Invalid order_by direction",
					fmt.Sprintf("Direction '%s' is not valid. Use 'asc' or 'desc'", tokens[1]),
				)
			}
			direction = dir
		}

		// Validate no extra tokens
		if len(tokens) > 2 {
			return "", NewInvalidArgumentError(
				"Invalid order_by format",
				fmt.Sprintf("Too many tokens in order_by clause '%s'. Expected format: 'field [asc|desc]'", part),
			)
		}

		gormParts = append(gormParts, fmt.Sprintf("%s %s", field, direction))
	}

	if len(gormParts) == 0 {
		return DefaultOrderBy, nil
	}

	return strings.Join(gormParts, ", "), nil
}
