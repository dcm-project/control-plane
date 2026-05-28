package service

import (
	"errors"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// catalogItemInstanceToStoreModel converts a CreateCatalogItemInstanceRequest to a store model
func catalogItemInstanceToStoreModel(id, resourceID, path string, req *CreateCatalogItemInstanceRequest) model.CatalogItemInstance {
	userValues := make([]model.UserValue, len(req.Spec.UserValues))
	for i, uv := range req.Spec.UserValues {
		userValues[i] = model.UserValue{
			Path:  uv.Path,
			Value: uv.Value,
		}
	}

	return model.CatalogItemInstance{
		ID:          id,
		ApiVersion:  req.ApiVersion,
		DisplayName: req.DisplayName,
		Spec: model.CatalogItemInstanceSpec{
			CatalogItemId: req.Spec.CatalogItemId,
			UserValues:    userValues,
		},
		ResourceID:        resourceID,
		Path:              path,
		SpecCatalogItemId: req.Spec.CatalogItemId,
	}
}

// catalogItemInstanceToAPIType converts a store model to an API type
func catalogItemInstanceToAPIType(m *model.CatalogItemInstance) v1alpha1.CatalogItemInstance {
	userValues := make([]v1alpha1.UserValue, len(m.Spec.UserValues))
	for i, uv := range m.Spec.UserValues {
		userValues[i] = v1alpha1.UserValue{
			Path:  uv.Path,
			Value: uv.Value,
		}
	}

	apiType := v1alpha1.CatalogItemInstance{
		ApiVersion:  m.ApiVersion,
		DisplayName: m.DisplayName,
		Spec: v1alpha1.CatalogItemInstanceSpec{
			CatalogItemId: m.Spec.CatalogItemId,
			UserValues:    userValues,
		},
		ResourceId: &m.ResourceID,
		Path:       &m.Path,
		Uid:        &m.ID,
		CreateTime: &m.CreateTime,
		UpdateTime: &m.UpdateTime,
	}

	return apiType
}

// mapCatalogItemInstanceStoreError converts store errors to service domain errors
func mapCatalogItemInstanceStoreError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, store.ErrCatalogItemInstanceNotFound):
		return ErrCatalogItemInstanceNotFound
	case errors.Is(err, store.ErrCatalogItemInstanceIDTaken):
		return ErrCatalogItemInstanceIDTaken
	case errors.Is(err, store.ErrCatalogItemNotFoundRef):
		return ErrCatalogItemNotFoundForInstance
	case errors.Is(err, store.ErrCatalogItemInstanceConflict):
		return ErrCatalogItemInstanceConflict
	default:
		return err
	}
}
