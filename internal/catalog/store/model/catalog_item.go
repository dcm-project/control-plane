// Package model defines the database models for catalog resources.
package model

import (
	"time"
)

// CatalogItem represents a catalog item in the database
type CatalogItem struct {
	ID          string          `gorm:"column:id;primaryKey"`
	ApiVersion  string          `gorm:"column:api_version;not null"`
	DisplayName string          `gorm:"column:display_name;not null"`
	Spec        CatalogItemSpec `gorm:"column:spec;type:jsonb;not null;serializer:json"`
	Path        string          `gorm:"column:path;not null"`
	CreateTime  time.Time       `gorm:"column:create_time;autoCreateTime"`
	UpdateTime  time.Time       `gorm:"column:update_time;autoUpdateTime"`

	// Indexed field for filtering
	SpecServiceType string       `gorm:"column:spec_service_type;not null;index"`
	ServiceTypeRef  *ServiceType `gorm:"foreignKey:SpecServiceType;references:ServiceType;constraint:OnDelete:RESTRICT"`
}

// CatalogItemList is a slice of CatalogItem for list results
type CatalogItemList []CatalogItem

// CatalogItemSpec represents the spec field of a catalog item
type CatalogItemSpec struct {
	ServiceType string               `json:"service_type"`
	Fields      []FieldConfiguration `json:"fields"`
}

// DependsOn defines conditional default based on another field's value
type DependsOn struct {
	Path          string           `json:"path"`
	AllowedValues map[string][]any `json:"allowed_values"`
}

// FieldConfiguration represents a field configuration within a catalog item
type FieldConfiguration struct {
	Path             string         `json:"path"`
	DisplayName      string         `json:"display_name,omitempty"`
	Editable         bool           `json:"editable"`
	Default          any            `json:"default,omitempty"`
	DependsOn        *DependsOn     `json:"depends_on,omitempty"`
	ValidationSchema map[string]any `json:"validation_schema,omitempty"`
}
