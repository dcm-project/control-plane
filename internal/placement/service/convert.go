package service

import (
	"fmt"
	"time"

	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"github.com/dcm-project/control-plane/internal/placement/types"
)

// storeModelToResource converts a database model to an API response type
func storeModelToResource(m *model.Resource) *types.Resource {
	idStr := m.ID
	path := fmt.Sprintf("resources/%s", idStr)

	resource := &types.Resource{
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
func resourceToStoreModel(req *types.Resource, id, path string) model.Resource {
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
