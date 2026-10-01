package service

import (
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// cpuQuantityPattern mirrors CpuResources min/max in
// api/catalog/v1alpha1/servicetypes/common.yaml: a positive count of whole
// cores ("2") or millicores ("500m").
var cpuQuantityPattern = regexp.MustCompile(`^[1-9][0-9]*m?$`)

const (
	cpuKey = "cpu"
	minKey = "min"
	maxKey = "max"

	millicoresPerCore = 1000
)

// validateResolvedSpec checks the quantity contracts a catalog item cannot express
// through per-field validation_schema. Catalog authors may leave a field without a
// schema, so without this the malformed value is only rejected asynchronously by
// the provider, after the instance has been created.
func validateResolvedSpec(spec map[string]any) error {
	return validateCPUQuantities(spec, "")
}

// validateCPUQuantities walks a resolved spec and validates every "cpu" object it
// finds, regardless of service type (container and database both nest one under
// "resources"). Values that are not objects (the cluster service type uses an
// integer cpu per node) are left to their own schemas.
func validateCPUQuantities(value any, path string) error {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(v)) {
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
	minMillicores, hasMin, err := cpuMillicores(cpu[minKey], joinSpecPath(path, minKey))
	if err != nil {
		return err
	}
	maxMillicores, hasMax, err := cpuMillicores(cpu[maxKey], joinSpecPath(path, maxKey))
	if err != nil {
		return err
	}
	if hasMin && hasMax && minMillicores > maxMillicores {
		return fmt.Errorf("%w: %s: %q is greater than %q", ErrCPUMinGreaterThanMax, path, cpu[minKey], cpu[maxKey])
	}
	return nil
}

// cpuMillicores converts a cpu quantity to millicores. It reports present=false for
// values that carry no quantity yet: an absent key, an empty string (the zero value
// of the service type template) or a ${resource.output} reference bound at apply time.
func cpuMillicores(value any, path string) (quantity int64, present bool, err error) {
	if value == nil {
		return 0, false, nil
	}
	str, ok := value.(string)
	if !ok {
		return 0, false, fmt.Errorf("%w: %s: must be a string, got %T", ErrInvalidCPUQuantity, path, value)
	}
	if str == "" {
		return 0, false, nil
	}
	if strings.Contains(str, "${") {
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
