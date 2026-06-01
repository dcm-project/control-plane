package service

import (
	"fmt"
	"time"

	apiv1alpha1 "github.com/dcm-project/control-plane/api/placement/v1alpha1"
	"github.com/dcm-project/control-plane/internal/placement/store/model"
)

// storeModelToResource converts a database model to an API response type
func storeModelToResource(m *model.Resource) *apiv1alpha1.Resource {
	idStr := m.ID
	path := fmt.Sprintf("resources/%s", idStr)

	resource := &apiv1alpha1.Resource{
		Id:                    &idStr,
		Path:                  &path,
		CatalogItemInstanceId: m.CatalogItemInstanceId,
		Spec:                  m.Spec,
		ProviderName:          m.ProviderName,
		ApprovalStatus:        m.ApprovalStatus,
		CreateTime:            PtrTime(m.CreateTime),
		UpdateTime:            PtrTime(m.UpdateTime),
	}
	return resource
}

// resourceToStoreModel converts an API request to a database model
func resourceToStoreModel(req *apiv1alpha1.Resource, id, path string) model.Resource {
	return model.Resource{
		ID:                    id,
		CatalogItemInstanceId: req.CatalogItemInstanceId,
		Spec:                  req.Spec,
		Path:                  path,
		ProviderName:          req.ProviderName,
		ApprovalStatus:        req.ApprovalStatus,
	}
}

// Helper functions for pointer conversions

func PtrTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
