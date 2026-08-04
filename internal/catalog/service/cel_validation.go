package service

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// celReferencePattern matches restricted catalog CEL: ${resourceName.outputField}
var celReferencePattern = regexp.MustCompile(`^\$\{([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\}$`)

type celReference struct {
	ResourceName string
	OutputField  string
}

func parseCELReference(value string) (celReference, bool, error) {
	if !strings.Contains(value, "${") {
		return celReference{}, false, nil
	}
	matches := celReferencePattern.FindStringSubmatch(value)
	if matches == nil {
		return celReference{}, true, fmt.Errorf("%w: %q", ErrInvalidCELExpression, value)
	}
	return celReference{
		ResourceName: matches[1],
		OutputField:  matches[2],
	}, true, nil
}

// serviceTypeOutputNames returns declared output field names from a service type.
// Reads optional spec.outputs until outputs are formally defined on ServiceType.
func serviceTypeOutputNames(st *model.ServiceType) map[string]bool {
	outputs := make(map[string]bool)
	raw, ok := st.Spec["outputs"]
	if !ok {
		return outputs
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return outputs
	}
	for name := range m {
		outputs[name] = true
	}
	return outputs
}

func validateCELReferenceValue(
	ctx context.Context,
	store store.Store,
	resourcesByName map[string]model.CatalogResource,
	consumerResourceName string,
	fieldPath string,
	value any,
) error {
	str, ok := value.(string)
	if !ok {
		return nil
	}

	ref, isCEL, err := parseCELReference(str)
	if err != nil {
		return err
	}
	if !isCEL {
		return nil
	}

	if ref.ResourceName == consumerResourceName {
		return fmt.Errorf("%w: field %s", ErrCELSelfReference, fieldPath)
	}

	source, ok := resourcesByName[ref.ResourceName]
	if !ok {
		return fmt.Errorf("%w: %s", ErrCELResourceNotFound, ref.ResourceName)
	}

	consumer := resourcesByName[consumerResourceName]
	if !slices.Contains(consumer.RequiresResources, ref.ResourceName) {
		return fmt.Errorf("%w: field %s references %s", ErrCELRequiresResourceMissing, fieldPath, ref.ResourceName)
	}

	sourceST, err := store.ServiceType().GetByServiceType(ctx, source.ServiceType)
	if err != nil {
		return ErrServiceTypeNotFound
	}

	outputs := serviceTypeOutputNames(sourceST)
	if len(outputs) == 0 {
		return fmt.Errorf("%w: service type %q has no declared outputs for %s.%s",
			ErrCELServiceTypeOutputNotFound, source.ServiceType, ref.ResourceName, ref.OutputField)
	}
	if !outputs[ref.OutputField] {
		return fmt.Errorf("%w: %s.%s", ErrCELServiceTypeOutputNotFound, ref.ResourceName, ref.OutputField)
	}

	return nil
}

func catalogResourcesByName(resources []model.CatalogResource) map[string]model.CatalogResource {
	byName := make(map[string]model.CatalogResource, len(resources))
	for _, r := range resources {
		byName[r.Name] = r
	}
	return byName
}
