package service

import (
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/dcm-project/control-plane/internal/cel"
)

// cpuQuantityPattern mirrors CpuResources min/max in
// api/catalog/v1alpha1/servicetypes/common.yaml: a positive count of whole
// cores ("2") or millicores ("500m").
var cpuQuantityPattern = regexp.MustCompile(`^[1-9][0-9]*m?$`)

const (
	cpuKey           = "cpu"
	minKey           = "min"
	maxKey           = "max"
	providerHintsKey = "provider_hints"

	millicoresPerCore = 1000
)

// validateResolvedSpec checks the quantity contracts a catalog item cannot express
// through per-field validation_schema. Catalog authors may leave a field without a
// schema, so without this the malformed value is only rejected asynchronously by
// the provider, after the instance has been created.
func validateResolvedSpec(spec map[string]any) error {
	return validateCPUQuantities(spec, "")
}

// validateCPUQuantities walks a resolved spec and validates every object-valued
// "cpu" key it finds at any depth, regardless of service type (container and
// database both nest one under "resources"). Matching on the key name assumes any
// object-valued "cpu" carries the CpuResources contract, which holds for the
// current service types: the only other cpu field is the cluster per-node integer,
// and values that are not objects are left to their own schemas. provider_hints is
// skipped because its contents are provider-specific and deliberately unconstrained.
func validateCPUQuantities(value any, path string) error {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(v)) {
			if key == providerHintsKey {
				continue
			}
			childPath := joinSpecPath(path, key)
			child := v[key]
			if cpu, ok := child.(map[string]any); ok && key == cpuKey {
				if err := validateCPUResources(cpu, childPath); err != nil {
					return err
				}
				continue
			}
			if err := validateCPUQuantities(child, childPath); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range v {
			if err := validateCPUQuantities(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateCPUResources validates the min/max pair of a single cpu object.
func validateCPUResources(cpu map[string]any, path string) error {
	minMillicores, hasMin, err := cpuQuantity(cpu, minKey, path)
	if err != nil {
		return err
	}
	maxMillicores, hasMax, err := cpuQuantity(cpu, maxKey, path)
	if err != nil {
		return err
	}
	if hasMin && hasMax && minMillicores > maxMillicores {
		return fmt.Errorf("%w: %s: %q is greater than %q", ErrCPUMinGreaterThanMax, path, cpu[minKey], cpu[maxKey])
	}
	return nil
}

// cpuQuantity converts one key of a cpu object to millicores. An absent key carries
// no quantity: whether min/max are required is the service type schema's contract,
// not this check's. A key present with an explicit null is still a malformed value.
func cpuQuantity(cpu map[string]any, key, path string) (quantity int64, present bool, err error) {
	value, ok := cpu[key]
	if !ok {
		return 0, false, nil
	}
	return cpuMillicores(value, joinSpecPath(path, key))
}

// cpuMillicores converts a cpu quantity to millicores. It reports present=false for
// values that carry no quantity yet: an empty string (the zero value of the service
// type template) or a ${resource.output} reference bound at apply time.
func cpuMillicores(value any, path string) (quantity int64, present bool, err error) {
	str, ok := value.(string)
	if !ok {
		return 0, false, fmt.Errorf("%w: %s: must be a string, got %T", ErrInvalidCPUQuantity, path, value)
	}
	if str == "" {
		return 0, false, nil
	}
	// Parse the reference instead of matching on "${": validateCELReferenceValue only
	// inspects a user value that is itself a string, so a malformed reference nested
	// in an object-valued resources.cpu override reaches us unchecked.
	if _, isReference, refErr := cel.ParseReference(str); isReference {
		if refErr != nil {
			return 0, false, fmt.Errorf("%w: %s: %q", ErrInvalidCELExpression, path, str)
		}
		return 0, false, nil
	}
	if !cpuQuantityPattern.MatchString(str) {
		return 0, false, fmt.Errorf("%w: %s: %q", ErrInvalidCPUQuantity, path, str)
	}

	digits, isMillicores := strings.CutSuffix(str, "m")
	n, parseErr := strconv.ParseInt(digits, 10, 64)
	if parseErr != nil {
		return 0, false, fmt.Errorf("%w: %s: %q is out of range", ErrInvalidCPUQuantity, path, str)
	}
	if isMillicores {
		return n, true, nil
	}
	if n > math.MaxInt64/millicoresPerCore {
		return 0, false, fmt.Errorf("%w: %s: %q is out of range", ErrInvalidCPUQuantity, path, str)
	}
	return n * millicoresPerCore, true, nil
}

// joinSpecPath appends a key to a dotted spec path.
func joinSpecPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
