package db

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/db/timestampmigration"
)

func chronologicalTimestampMigration() migration {
	return migration{
		version:     35,
		description: "canonical chronological history and audit timestamps",
		preflight:   normalizeChronologicalTimestamps,
	}
}

func normalizeChronologicalTimestamps(tx *sql.Tx) error {
	return timestampmigration.Normalize(tx)
}
