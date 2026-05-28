// Package store provides database access for catalog resources.
package store

import (
	"log/slog"

	"gorm.io/gorm"
)

// Store provides access to all resource stores
type Store interface {
	ServiceType() ServiceTypeStore
	CatalogItem() CatalogItemStore
	CatalogItemInstance() CatalogItemInstanceStore
	Close() error
}

// DataStore implements the Store interface
type DataStore struct {
	db                  *gorm.DB
	serviceType         ServiceTypeStore
	catalogItem         CatalogItemStore
	catalogItemInstance CatalogItemInstanceStore
}

// NewStore creates a new DataStore
func NewStore(db *gorm.DB, logger *slog.Logger) Store {
	storeLogger := logger.With("component", "store")
	return &DataStore{
		db:                  db,
		serviceType:         NewServiceTypeStore(db, storeLogger),
		catalogItem:         NewCatalogItemStore(db, storeLogger),
		catalogItemInstance: NewCatalogItemInstanceStore(db, storeLogger),
	}
}

// ServiceType returns the ServiceType store
func (s *DataStore) ServiceType() ServiceTypeStore {
	return s.serviceType
}

// CatalogItem returns the CatalogItem store
func (s *DataStore) CatalogItem() CatalogItemStore {
	return s.catalogItem
}

// CatalogItemInstance returns the CatalogItemInstance store
func (s *DataStore) CatalogItemInstance() CatalogItemInstanceStore {
	return s.catalogItemInstance
}

// Close closes the database connection
func (s *DataStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
