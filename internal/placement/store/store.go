// Package store provides data persistence operations for resources.
package store

import (
	"gorm.io/gorm"
)

// Store provides access to all repositories
type Store interface {
	Close() error
	Resource() Resource
}

// DataStore is the concrete implementation of Store
type DataStore struct {
	db       *gorm.DB
	resource Resource
}

// NewStore creates a new DataStore with initialized repositories
func NewStore(db *gorm.DB) Store {
	return &DataStore{
		db:       db,
		resource: NewResource(db),
	}
}

// Close closes the database connection
func (s *DataStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Resource returns the Resource repository
func (s *DataStore) Resource() Resource {
	return s.resource
}
