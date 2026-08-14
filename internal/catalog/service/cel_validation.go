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

// serviceTypeTemplateHasField reports whether fieldName exists on the service type template spec.
func serviceTypeTemplateHasField(st *model.ServiceType, fieldName string) bool {
	if st == nil || st.Spec == nil {
		return false
	}
	_, err := getNestedValue(st.Spec, fieldName)
	return err == nil
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

	if !serviceTypeTemplateHasField(sourceST, ref.OutputField) {
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
