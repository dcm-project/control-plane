package store

import "gorm.io/gorm"

// applyCatalogItemServiceTypeFilter restricts results to catalog items whose spec.resources
// includes at least one entry with the given service_type.
func applyCatalogItemServiceTypeFilter(query *gorm.DB, serviceType string) *gorm.DB {
	switch query.Dialector.Name() {
	case "postgres":
		return query.Where(`EXISTS (
			SELECT 1 FROM jsonb_array_elements(spec->'resources') AS resource
			WHERE resource->>'service_type' = ?
		)`, serviceType)
	case "sqlite":
		return query.Where(`EXISTS (
			SELECT 1 FROM json_each(spec, '$.resources') AS resource
			WHERE json_extract(resource.value, '$.service_type') = ?
		)`, serviceType)
	default:
		return query
	}
}
