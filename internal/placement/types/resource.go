// Package types holds domain models for placement resources.
package types

import "time"

// Resource is a placement resource managed in-process by PlacementService.
type Resource struct {
	ApprovalStatus        *string        `json:"approval_status,omitempty"`
	CatalogItemInstanceId string         `json:"catalog_item_instance_id"`
	CreateTime            *time.Time     `json:"create_time,omitempty"`
	Id                    *string        `json:"id,omitempty"`
	Path                  *string        `json:"path,omitempty"`
	ProviderName          *string        `json:"provider_name,omitempty"`
	Spec                  map[string]any `json:"spec"`
	UpdateTime            *time.Time     `json:"update_time,omitempty"`
}
