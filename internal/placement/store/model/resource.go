// Package model defines the database models for resource storage.
package model

import (
	"time"
)

// Resource represents a resource provisioning request
type Resource struct {
	ID                    string         `gorm:"primaryKey;type:varchar(63)"`
	CatalogItemInstanceId string         `gorm:"column:catalog_item_instance_id;not null"`
	Spec                  map[string]any `gorm:"column:original_spec;type:jsonb;serializer:json;not null"`
	ProviderName          *string        `gorm:"column:provider_name;not null"`
	ApprovalStatus        *string        `gorm:"column:approval_status;not null"`
	Path                  string         `gorm:"column:path;not null"`
	CreateTime            time.Time      `gorm:"column:create_time;autoCreateTime"`
	UpdateTime            time.Time      `gorm:"column:update_time;autoUpdateTime"`
}

// ResourceList is a list of requests
type ResourceList []Resource
