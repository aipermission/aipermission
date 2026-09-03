package db

// s3UploadProjectionScrubMigration removes object bodies that older builds
// copied into operator-facing projections. The encrypted action payload stays
// intact so pending work can still execute after the upgrade.
var s3UploadProjectionScrubMigration = migration{
	version:     20,
	description: "scrub S3 upload content projections",
	statements: []string{
		`UPDATE connector_action_requests
		 SET input_json = json_set(input_json, '$.content_text', '[REDACTED]')
		 WHERE connector_kind = 's3'
		   AND action_name = 'upload_object'
		   AND json_valid(input_json)
		   AND json_type(input_json, '$.content_text') IS NOT NULL;`,
		`UPDATE connector_action_requests
		 SET input_json = json_set(input_json, '$.content_base64', '[REDACTED]')
		 WHERE connector_kind = 's3'
		   AND action_name = 'upload_object'
		   AND json_valid(input_json)
		   AND json_type(input_json, '$.content_base64') IS NOT NULL;`,
		`UPDATE connector_action_requests
		 SET preview_json = json_set(preview_json, '$.content_text', '[REDACTED]')
		 WHERE connector_kind = 's3'
		   AND action_name = 'upload_object'
		   AND json_valid(preview_json)
		   AND json_type(preview_json, '$.content_text') IS NOT NULL;`,
		`UPDATE connector_action_requests
		 SET preview_json = json_set(preview_json, '$.content_base64', '[REDACTED]')
		 WHERE connector_kind = 's3'
		   AND action_name = 'upload_object'
		   AND json_valid(preview_json)
		   AND json_type(preview_json, '$.content_base64') IS NOT NULL;`,
		`UPDATE history_entries
		 SET input_json = json_set(input_json, '$.content_text', '[REDACTED]')
		 WHERE connector_kind = 's3'
		   AND action_name = 'upload_object'
		   AND json_valid(input_json)
		   AND json_type(input_json, '$.content_text') IS NOT NULL;`,
		`UPDATE history_entries
		 SET input_json = json_set(input_json, '$.content_base64', '[REDACTED]')
		 WHERE connector_kind = 's3'
		   AND action_name = 'upload_object'
		   AND json_valid(input_json)
		   AND json_type(input_json, '$.content_base64') IS NOT NULL;`,
		`UPDATE history_entries
		 SET preview_json = json_set(preview_json, '$.content_text', '[REDACTED]')
		 WHERE connector_kind = 's3'
		   AND action_name = 'upload_object'
		   AND json_valid(preview_json)
		   AND json_type(preview_json, '$.content_text') IS NOT NULL;`,
		`UPDATE history_entries
		 SET preview_json = json_set(preview_json, '$.content_base64', '[REDACTED]')
		 WHERE connector_kind = 's3'
		   AND action_name = 'upload_object'
		   AND json_valid(preview_json)
		   AND json_type(preview_json, '$.content_base64') IS NOT NULL;`,
	},
}
