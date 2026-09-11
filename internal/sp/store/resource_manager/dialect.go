package store

import (
	"gorm.io/gorm"
)

func isPostgres(db *gorm.DB) bool {
	return db != nil && db.Name() == "postgres"
}
