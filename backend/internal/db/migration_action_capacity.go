package db

import "github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"

func actionCapacityProjectionMigration() migration {
	return migration{
		version:     43,
		description: "transactional connector action storage usage projection",
		statements:  actioncapacity.ProjectionStatements(),
	}
}
