// Package model defines the database models for resource storage.
package model

import (
	"time"
)

// Resource represents a resource provisioning request within a placement run.
type Resource struct {
	ID                    string         `gorm:"primaryKey;type:varchar(63)"`
	RunID                 string         `gorm:"column:run_id;type:varchar(63);index;not null"`
	CatalogItemInstanceId string         `gorm:"column:catalog_item_instance_id;not null"`
	Name                  string         `gorm:"column:name;type:varchar(63);not null"`
	Spec                  map[string]any `gorm:"column:original_spec;type:jsonb;serializer:json;not null"`
	RequiresResources     []string       `gorm:"column:requires_resources;type:jsonb;serializer:json"`
	DagLevel              int            `gorm:"column:dag_level;not null;default:0"`
	Status                string         `gorm:"column:status;type:varchar(63);not null;default:PENDING"`
	ProviderName          *string        `gorm:"column:provider_name;not null"`
	ApprovalStatus        *string        `gorm:"column:approval_status;not null"`
	Path                  string         `gorm:"column:path;not null"`
	CreateTime            time.Time      `gorm:"column:create_time;autoCreateTime"`
	UpdateTime            time.Time      `gorm:"column:update_time;autoUpdateTime"`
}

// ResourceList is a list of requests
type ResourceList []Resource
