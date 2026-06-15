package service

import (
	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

func catalogItemSpecAPIToModel(spec v1alpha1.CatalogItemSpec) model.CatalogItemSpec {
	return model.CatalogItemSpec{
		Resources: catalogResourceAPIToModel(spec.Resources),
	}
}

func catalogItemSpecModelToAPI(spec model.CatalogItemSpec) v1alpha1.CatalogItemSpec {
	return v1alpha1.CatalogItemSpec{
		Resources: catalogResourceModelToAPI(spec.Resources),
	}
}

func catalogResourceAPIToModel(resources []v1alpha1.CatalogResource) []model.CatalogResource {
	out := make([]model.CatalogResource, len(resources))
	for i, r := range resources {
		out[i] = model.CatalogResource{
			Name:        r.Name,
			ServiceType: r.ServiceType,
		}
		if r.RequiresResources != nil {
			out[i].RequiresResources = append([]string(nil), *r.RequiresResources...)
		}
		if r.Fields != nil {
			out[i].Fields = FieldConfigurationsToModel(*r.Fields)
		}
	}
	return out
}

func catalogResourceModelToAPI(resources []model.CatalogResource) []v1alpha1.CatalogResource {
	out := make([]v1alpha1.CatalogResource, len(resources))
	for i, r := range resources {
		out[i] = v1alpha1.CatalogResource{
			Name:        r.Name,
			ServiceType: r.ServiceType,
		}
		if len(r.RequiresResources) > 0 {
			req := append([]string(nil), r.RequiresResources...)
			out[i].RequiresResources = &req
		}
		if len(r.Fields) > 0 {
			fields := FieldConfigurationsFromModel(r.Fields)
			out[i].Fields = &fields
		}
	}
	return out
}
