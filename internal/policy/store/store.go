package store

import "gorm.io/gorm"

type Store interface {
	Close() error
	Policy() Policy
}

type DataStore struct {
	db     *gorm.DB
	policy Policy
}

func NewStore(db *gorm.DB) Store {
	return &DataStore{
		db:     db,
		policy: NewPolicy(db),
	}
}

func (s *DataStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *DataStore) Policy() Policy {
	return s.policy
}
