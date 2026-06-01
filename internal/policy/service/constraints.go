package service

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/brunoga/deep/v4"
	"github.com/dcm-project/control-plane/internal/policy/opa"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ConstraintContext tracks JSON Schema constraints set by higher-priority policies
type ConstraintContext struct {
	constrainedFieldsByFieldPath map[string]map[string]any // field path → JSON Schema keywords
	policyIdByFieldPath          map[string]string         // field path → policy ID that set it
	serviceProviderConstraints   *AccumulatedSPConstraints
}

// AccumulatedSPConstraints tracks accumulated service provider constraints
type AccumulatedSPConstraints struct {
	AllowList   []string // Intersection of all allow lists
	Patterns    []string // All patterns (ANDed)
	SetByPolicy string   // Policy ID that first set SP constraints
}

// ConstraintConflictError is returned by MergeConstraints when a lower-priority
// policy would loosen a constraint set by a higher-priority policy.
type ConstraintConflictError struct {
	FieldPath   string // field path that caused the conflict
	SetByPolicy string // policy ID that set the existing constraint
	Reason      string // human-readable detail
}

func (e *ConstraintConflictError) Error() string {
	return e.Reason
}

// NewConstraintContext creates a new ConstraintContext
func NewConstraintContext() *ConstraintContext {
	return &ConstraintContext{
		constrainedFieldsByFieldPath: make(map[string]map[string]any),
		policyIdByFieldPath:          make(map[string]string),
	}
}

// MergeConstraints merges new per-field JSON Schema constraints from a policy.
// Constraints can only be tightened, never loosened. Returns an error if a
// constraint would be loosened.
func (c *ConstraintContext) MergeConstraints(newConstraints map[string]any, policyID string) error {
	for fieldPath, constraint := range newConstraints {
		newConstraint, ok := constraint.(map[string]any)
		if !ok {
			continue
		}

		existingConstrains, hasExistingConstrains := c.constrainedFieldsByFieldPath[fieldPath]
		if !hasExistingConstrains {
			// First constraint for this field — just store it
			c.constrainedFieldsByFieldPath[fieldPath] = deepCopySchemaMap(newConstraint)
			c.policyIdByFieldPath[fieldPath] = policyID
			continue
		}

		// Merge each keyword, enforcing tightening-only
		mergedConstraints, err := mergeSchemaKeywords(existingConstrains, newConstraint, fieldPath, c.policyIdByFieldPath[fieldPath])
		if err != nil {
			return err
		}
		c.constrainedFieldsByFieldPath[fieldPath] = mergedConstraints
	}
	return nil
}

// ValidatePatch validates each field in the patch against the accumulated
// constraints using JSON Schema validation. Returns a list of violations.
// Uses a single compiler and caches compiled schemas per field path for the duration of the call.
func (c *ConstraintContext) ValidatePatch(patch map[string]any) []ConstraintViolation {
	var violations []ConstraintViolation
	compiler := jsonschema.NewCompiler()
	compiled := make(map[string]*jsonschema.Schema)
	c.validatePatchRecursive("", patch, &violations, compiler, compiled)
	return violations
}

// validatePatchRecursive recursively validates patch fields against constraints
func (c *ConstraintContext) validatePatchRecursive(
	prefix string,
	patch map[string]any,
	violations *[]ConstraintViolation,
	compiler *jsonschema.Compiler,
	compiled map[string]*jsonschema.Schema,
) {
	for key, value := range patch {
		fieldPath := key
		if prefix != "" {
			fieldPath = prefix + "." + key
		}

		// Check if there's a constraint for this field
		if schemaMap, exists := c.constrainedFieldsByFieldPath[fieldPath]; exists {
			comp, err := getOrCompileSchema(compiler, compiled, fieldPath, schemaMap)
			if err != nil {
				*violations = append(*violations, ConstraintViolation{
					FieldPath:   fieldPath,
					Reason:      err.Error(),
					SetByPolicy: c.policyIdByFieldPath[fieldPath],
				})
			} else if err := comp.Validate(value); err != nil {
				*violations = append(*violations, ConstraintViolation{
					FieldPath:   fieldPath,
					Reason:      fmt.Sprintf("value %v violates constraint: %v", value, err),
					SetByPolicy: c.policyIdByFieldPath[fieldPath],
				})
			}
		}

		// Recurse into nested maps
		if nestedMap, ok := value.(map[string]any); ok {
			c.validatePatchRecursive(fieldPath, nestedMap, violations, compiler, compiled)
		}
	}
}

// MergeSPConstraints merges service provider constraints from a policy decision.
// If sp is nil or has neither allow list nor patterns, it is a no-op.
// Allow lists are intersected once; all patterns are appended (ANDed).
func (c *ConstraintContext) MergeSPConstraints(sp *opa.ServiceProviderConstraints, policyID string) error {
	if sp == nil {
		return nil
	}
	allowList := sp.AllowList
	patterns := sp.Patterns
	if len(allowList) == 0 && len(patterns) == 0 {
		return nil
	}
	if c.serviceProviderConstraints == nil {
		c.serviceProviderConstraints = &AccumulatedSPConstraints{
			AllowList:   allowList,
			Patterns:    append([]string(nil), patterns...),
			SetByPolicy: policyID,
		}
		return nil
	}

	// Intersect allow lists if both exist
	if len(allowList) > 0 && len(c.serviceProviderConstraints.AllowList) > 0 {
		intersected := intersectStringSlices(c.serviceProviderConstraints.AllowList, allowList)
		if len(intersected) == 0 {
			return fmt.Errorf("service provider allow list intersection is empty: "+
				"policy '%s' allows %v but existing constraints from policy '%s' allow %v",
				policyID, allowList, c.serviceProviderConstraints.SetByPolicy, c.serviceProviderConstraints.AllowList)
		}
		c.serviceProviderConstraints.AllowList = intersected
	} else if len(allowList) > 0 {
		c.serviceProviderConstraints.AllowList = allowList
	}

	// AND patterns
	c.serviceProviderConstraints.Patterns = append(c.serviceProviderConstraints.Patterns, patterns...)

	return nil
}

// ValidateServiceProvider checks a provider against accumulated SP constraints
func (c *ConstraintContext) ValidateServiceProvider(provider string) error {
	if c.serviceProviderConstraints == nil || provider == "" {
		return nil
	}

	sp := c.serviceProviderConstraints

	// Check allow list
	if len(sp.AllowList) > 0 {
		found := slices.Contains(sp.AllowList, provider)
		if !found {
			return fmt.Errorf("provider '%s' is not in the allowed list %v (constrained by policy '%s')",
				provider, sp.AllowList, sp.SetByPolicy)
		}
	}

	// Check patterns
	for _, pattern := range sp.Patterns {
		matched, err := regexp.MatchString(pattern, provider)
		if err != nil {
			return fmt.Errorf("invalid service provider pattern '%s': %v", pattern, err)
		}
		if !matched {
			return fmt.Errorf("provider '%s' does not match required pattern '%s'", provider, pattern)
		}
	}

	return nil
}

// GetConstraintsMap returns the accumulated constraints for inclusion in OPA input
func (c *ConstraintContext) GetConstraintsMap() map[string]any {
	if len(c.constrainedFieldsByFieldPath) == 0 {
		return nil
	}
	result := make(map[string]any, len(c.constrainedFieldsByFieldPath))
	for k, v := range c.constrainedFieldsByFieldPath {
		result[k] = v
	}
	return result
}

// GetSPConstraintsMap returns SP constraints for inclusion in OPA input
func (c *ConstraintContext) GetSPConstraintsMap() map[string]any {
	if c.serviceProviderConstraints == nil {
		return nil
	}
	result := make(map[string]any)
	if len(c.serviceProviderConstraints.AllowList) > 0 {
		result["allow_list"] = c.serviceProviderConstraints.AllowList
	}
	if len(c.serviceProviderConstraints.Patterns) > 0 {
		// Combine patterns into a single regex with AND semantics
		result["patterns"] = c.serviceProviderConstraints.Patterns
	}
	return result
}

// mergeSchemaKeywords merges JSON Schema keywords, enforcing tightening-only.
func mergeSchemaKeywords(existing, new map[string]any, fieldPath, existingPolicyID string) (map[string]any, error) {
	merged := deepCopySchemaMap(existing)

	for keyword, newVal := range new {
		existingVal, hasExisting := merged[keyword]

		if !hasExisting {
			// New keyword — just add it
			merged[keyword] = newVal
			continue
		}

		switch keyword {
		case "const":
			// const must be identical
			if !jsonValuesEqual(existingVal, newVal) {
				return nil, &ConstraintConflictError{
					FieldPath:   fieldPath,
					SetByPolicy: existingPolicyID,
					Reason: fmt.Sprintf(
						"cannot change const constraint on field '%s': existing value %v (set by policy '%s') differs from new value %v",
						fieldPath, existingVal, existingPolicyID, newVal,
					),
				}
			}

		case "enum":
			// Intersection of value sets
			existingEnum, ok1 := toSlice(existingVal)
			newEnum, ok2 := toSlice(newVal)
			if ok1 && ok2 {
				intersected := intersectAnySlices(existingEnum, newEnum)
				if len(intersected) == 0 {
					return nil, &ConstraintConflictError{
						FieldPath:   fieldPath,
						SetByPolicy: existingPolicyID,
						Reason: fmt.Sprintf(
							"enum constraint intersection is empty for field '%s': existing %v (set by policy '%s'), new %v",
							fieldPath, existingEnum, existingPolicyID, newEnum,
						),
					}
				}
				merged[keyword] = intersected
			}

		case "minimum", "minLength", "minItems", "minProperties":
			// Can only increase
			existingNum, ok1 := toFloat64(existingVal)
			newNum, ok2 := toFloat64(newVal)
			if ok1 && ok2 {
				if newNum < existingNum {
					return nil, &ConstraintConflictError{
						FieldPath:   fieldPath,
						SetByPolicy: existingPolicyID,
						Reason: fmt.Sprintf(
							"cannot loosen %s constraint on field '%s': existing %v (set by policy '%s'), attempted %v",
							keyword, fieldPath, existingNum, existingPolicyID, newNum,
						),
					}
				}
				merged[keyword] = math.Max(existingNum, newNum)
			}

		case "maximum", "maxLength", "maxItems", "maxProperties":
			// Can only decrease
			existingNum, ok1 := toFloat64(existingVal)
			newNum, ok2 := toFloat64(newVal)
			if ok1 && ok2 {
				if newNum > existingNum {
					return nil, &ConstraintConflictError{
						FieldPath:   fieldPath,
						SetByPolicy: existingPolicyID,
						Reason: fmt.Sprintf(
							"cannot loosen %s constraint on field '%s': existing %v (set by policy '%s'), attempted %v",
							keyword, fieldPath, existingNum, existingPolicyID, newNum,
						),
					}
				}
				merged[keyword] = math.Min(existingNum, newNum)
			}

		case "exclusiveMinimum":
			existingNum, ok1 := toFloat64(existingVal)
			newNum, ok2 := toFloat64(newVal)
			if ok1 && ok2 {
				if newNum < existingNum {
					return nil, &ConstraintConflictError{
						FieldPath:   fieldPath,
						SetByPolicy: existingPolicyID,
						Reason: fmt.Sprintf(
							"cannot loosen %s constraint on field '%s': existing %v (set by policy '%s'), attempted %v",
							keyword, fieldPath, existingNum, existingPolicyID, newNum,
						),
					}
				}
				merged[keyword] = math.Max(existingNum, newNum)
			}

		case "exclusiveMaximum":
			existingNum, ok1 := toFloat64(existingVal)
			newNum, ok2 := toFloat64(newVal)
			if ok1 && ok2 {
				if newNum > existingNum {
					return nil, &ConstraintConflictError{
						FieldPath:   fieldPath,
						SetByPolicy: existingPolicyID,
						Reason: fmt.Sprintf(
							"cannot loosen %s constraint on field '%s': existing %v (set by policy '%s'), attempted %v",
							keyword, fieldPath, existingNum, existingPolicyID, newNum,
						),
					}
				}
				merged[keyword] = math.Min(existingNum, newNum)
			}

		case "pattern":
			// Additional patterns are ANDed — store as allOf with pattern constraints
			existingPattern, ok1 := existingVal.(string)
			newPattern, ok2 := newVal.(string)
			if ok1 && ok2 && existingPattern != newPattern {
				// Create an allOf that combines both patterns
				if allOf, ok := merged["allOf"].([]any); ok {
					// Append to existing allOf
					merged["allOf"] = append(allOf, map[string]any{"pattern": newPattern})
				} else {
					merged["allOf"] = []any{
						map[string]any{"pattern": existingPattern},
						map[string]any{"pattern": newPattern},
					}
				}
				// Keep the first pattern in the top-level for backwards compat
			}

		case "multipleOf":
			// New multipleOf must be a multiple of the existing value
			existingNum, ok1 := toFloat64(existingVal)
			newNum, ok2 := toFloat64(newVal)
			if ok1 && ok2 && existingNum != 0 {
				remainder := math.Mod(newNum, existingNum)
				if remainder != 0 {
					return nil, &ConstraintConflictError{
						FieldPath:   fieldPath,
						SetByPolicy: existingPolicyID,
						Reason: fmt.Sprintf(
							"multipleOf %v is not a multiple of existing %v on field '%s' (set by policy '%s')",
							newNum, existingNum, fieldPath, existingPolicyID,
						),
					}
				}
				// Keep the new (more restrictive) multipleOf
				merged[keyword] = newNum
			}

		default:
			// Unknown or unmerged keywords: new value overrides. This is intentionally
			// not guaranteed to be tightening (e.g. enum vs const on same field).
			merged[keyword] = newVal
		}
	}

	return merged, nil
}

// getOrCompileSchema returns a compiled schema for the field path, compiling and caching
// it with the shared compiler on first use.
func getOrCompileSchema(
	compiler *jsonschema.Compiler,
	compiled map[string]*jsonschema.Schema,
	fieldPath string,
	schemaMap map[string]any,
) (*jsonschema.Schema, error) {
	if s := compiled[fieldPath]; s != nil {
		return s, nil
	}
	schemaBytes, err := json.Marshal(schemaMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %v", err)
	}
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(string(schemaBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema: %v", err)
	}
	// Unique URI per field path so the compiler can hold multiple schemas
	uri := "file:///constraint/" + strings.ReplaceAll(fieldPath, ".", "_")
	if err := compiler.AddResource(uri, schema); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %v", err)
	}
	comp, err := compiler.Compile(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %v", err)
	}
	compiled[fieldPath] = comp
	return comp, nil
}

// Helper functions

// deepCopySchemaMap creates a deep copy of a schema map using github.com/brunoga/deep.
// Falls back to recursive copy if deep.Copy fails (e.g. for non-standard types).
func deepCopySchemaMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	copied, err := deep.Copy(m)
	if err != nil {
		return deepCopySchemaMapRecursive(m)
	}
	return copied
}

// deepCopySchemaMapRecursive performs a recursive deep copy (fallback when deep.Copy fails).
func deepCopySchemaMapRecursive(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = deepCopySchemaValue(v)
	}
	return result
}

func deepCopySchemaValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopySchemaMapRecursive(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = deepCopySchemaValue(item)
		}
		return out
	default:
		return v
	}
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func toSlice(v any) ([]any, bool) {
	if s, ok := v.([]any); ok {
		return s, true
	}
	return nil, false
}

func jsonValuesEqual(a, b any) bool {
	aBytes, err1 := json.Marshal(a)
	bBytes, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
	return string(aBytes) == string(bBytes)
}

func intersectAnySlices(a, b []any) []any {
	var result []any
	for _, av := range a {
		for _, bv := range b {
			if jsonValuesEqual(av, bv) {
				result = append(result, av)
				break
			}
		}
	}
	return result
}

func intersectStringSlices(a, b []string) []string {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	var result []string
	for _, s := range b {
		if set[s] {
			result = append(result, s)
		}
	}
	return result
}
