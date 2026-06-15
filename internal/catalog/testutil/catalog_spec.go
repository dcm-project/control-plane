// Package testutil provides helpers shared by catalog tests.
package testutil

import (
	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// DefaultResourceName is the single-resource name used by most catalog tests.
const DefaultResourceName = "main"

func ptrAPIFields(fields []v1alpha1.FieldConfiguration) *[]v1alpha1.FieldConfiguration {
	return &fields
}

// CatalogSpec builds a single-resource CatalogItemSpec for API-layer tests.
func CatalogSpec(serviceType string, fields []v1alpha1.FieldConfiguration) v1alpha1.CatalogItemSpec {
	return v1alpha1.CatalogItemSpec{
		Resources: []v1alpha1.CatalogResource{{
			Name:        DefaultResourceName,
			ServiceType: serviceType,
			Fields:      ptrAPIFields(fields),
		}},
	}
}

// CatalogSpecVM builds a single-resource VM catalog item spec.
func CatalogSpecVM(fields []v1alpha1.FieldConfiguration) v1alpha1.CatalogItemSpec {
	return CatalogSpec("vm", fields)
}

// CatalogSpecContainer builds a single-resource container catalog item spec.
func CatalogSpecContainer(fields []v1alpha1.FieldConfiguration) v1alpha1.CatalogItemSpec {
	return CatalogSpec("container", fields)
}

// PtrCatalogSpec returns a pointer to CatalogSpec.
func PtrCatalogSpec(serviceType string, fields []v1alpha1.FieldConfiguration) *v1alpha1.CatalogItemSpec {
	s := CatalogSpec(serviceType, fields)
	return &s
}

// ModelCatalogSpec builds a single-resource CatalogItemSpec for store-layer tests.
func ModelCatalogSpec(serviceType string, fields []model.FieldConfiguration) model.CatalogItemSpec {
	return model.CatalogItemSpec{
		Resources: []model.CatalogResource{{
			Name:        DefaultResourceName,
			ServiceType: serviceType,
			Fields:      fields,
		}},
	}
}
