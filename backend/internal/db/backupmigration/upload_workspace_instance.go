package backupmigration

func UploadWorkspaceInstanceStatements() []string {
	return []string{
		`ALTER TABLE backup_upload_operations
			ADD COLUMN workspace_instance_id TEXT NOT NULL DEFAULT '';`,
		`UPDATE backup_upload_operations
			SET workspace_instance_id = COALESCE((
				SELECT TRIM(value) FROM settings WHERE key = 'ui_retry_instance_id'
			), '')
			WHERE workspace_instance_id = '';`,
	}
}
