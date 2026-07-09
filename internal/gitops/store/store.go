package store

import "gorm.io/gorm"

type Store interface {
	Close() error
	GitRepository() GitRepository
}

type DataStore struct {
	db            *gorm.DB
	gitRepository GitRepository
}

func NewStore(db *gorm.DB) Store {
	return &DataStore{
		db:            db,
		gitRepository: NewGitRepository(db),
	}
}

func (s *DataStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *DataStore) GitRepository() GitRepository {
	return s.gitRepository
}
