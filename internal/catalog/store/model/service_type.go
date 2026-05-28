package model

import (
	"time"
)

// ServiceType represents a service type definition in the database
type ServiceType struct {
	ID          string         `gorm:"column:id;primaryKey"`
	ApiVersion  string         `gorm:"column:api_version;not null"`
	ServiceType string         `gorm:"column:service_type;not null;uniqueIndex"`
	Metadata    Metadata       `gorm:"column:metadata;type:jsonb;serializer:json"`
	Spec        map[string]any `gorm:"column:spec;type:jsonb;not null;serializer:json"`
	Path        string         `gorm:"column:path;not null"`
	CreateTime  time.Time      `gorm:"column:create_time;autoCreateTime"`
	UpdateTime  time.Time      `gorm:"column:update_time;autoUpdateTime"`
}

type ServiceTypeList []ServiceType

// Metadata represents the metadata field with labels
type Metadata struct {
	Labels map[string]string `json:"labels,omitempty"`
}
