package api

type importDatabaseRequest struct {
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

type transientBackupRestoreRequest struct {
	BaseURL          string `json:"base_url"`
	Token            string `json:"token"`
	StreamID         string `json:"stream_id"`
	BackupID         string `json:"backup_id"`
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}
