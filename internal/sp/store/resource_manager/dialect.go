package store

import (
	"gorm.io/gorm"
)

func isPostgres(db *gorm.DB) bool {
	return db != nil && db.Name() == "postgres"
}

// pendingDeletionClaimOrder prefers rows never attempted (NULL
// last_deletion_attempt), then the oldest attempt time, with
// deletion_requested_at as a deterministic tie-breaker.
func pendingDeletionClaimOrder(db *gorm.DB) string {
	if isPostgres(db) {
		return "last_deletion_attempt ASC NULLS FIRST, deletion_requested_at ASC"
	}
	// SQLite sorts NULL before other values in ASC order by default.
	return "last_deletion_attempt ASC, deletion_requested_at ASC"
}
