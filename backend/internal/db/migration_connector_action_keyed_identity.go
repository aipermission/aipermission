package db

var connectorActionKeyedIdentityMigration = migration{
	version:     25,
	description: "remove legacy unkeyed connector action identity digests",
	statements: []string{
		`UPDATE connector_action_requests
		 SET idempotency_identity_hash = 'legacy-invalid:' || lower(hex(randomblob(32)))
		 WHERE idempotency_identity_hash <> ''
		   AND idempotency_identity_hash NOT LIKE 'h1:%';`,
	},
}
