package service

import (
	"errors"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// catalogItemToStoreModel converts a CreateCatalogItemRequest to a store model
func catalogItemToStoreModel(id, path string, req *CreateCatalogItemRequest) model.CatalogItem {
	fields := FieldConfigurationsToModel(*req.Spec.Fields)

	storeModel := model.CatalogItem{
		ID:          id,
		ApiVersion:  req.ApiVersion,
		DisplayName: req.DisplayName,
		Spec: model.CatalogItemSpec{
			ServiceType: *req.Spec.ServiceType,
			Fields:      fields,
		},
		Path:            path,
		SpecServiceType: *req.Spec.ServiceType, // Indexed field for filtering
	}

	return storeModel
}

// catalogItemToAPIType converts a store model to an API type
func catalogItemToAPIType(m *model.CatalogItem) v1alpha1.CatalogItem {
	fields := FieldConfigurationsFromModel(m.Spec.Fields)

	apiType := v1alpha1.CatalogItem{
		ApiVersion:  &m.ApiVersion,
		DisplayName: &m.DisplayName,
		Spec: &v1alpha1.CatalogItemSpec{
			ServiceType: &m.Spec.ServiceType,
			Fields:      &fields,
		},
		Path:       &m.Path,
		Uid:        &m.ID,
		CreateTime: &m.CreateTime,
		UpdateTime: &m.UpdateTime,
	}

	return apiType
}

// mapCatalogItemStoreError converts store errors to service domain errors
func mapCatalogItemStoreError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, store.ErrCatalogItemNotFound):
		return ErrCatalogItemNotFound
	case errors.Is(err, store.ErrCatalogItemIDTaken):
		return ErrCatalogItemIDTaken
	case errors.Is(err, store.ErrCatalogItemHasInstances):
		return ErrCatalogItemHasInstances
	case errors.Is(err, store.ErrServiceTypeNotFound):
		return ErrServiceTypeNotFound
	default:
		return err
	}
}
