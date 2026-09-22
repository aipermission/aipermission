package backupmigration

import (
	"database/sql"
	"testing"

	_ "github.com/SE-I-T-Digital/go-sqlcipher"
)

func TestUploadWorkspaceInstanceStatementsBindExistingOperations(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, statement := range []string{
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO settings(key, value) VALUES ('ui_retry_instance_id', ' instance-a ')`,
		`CREATE TABLE backup_upload_operations (idempotency_key TEXT PRIMARY KEY)`,
		`INSERT INTO backup_upload_operations(idempotency_key) VALUES ('upload-a')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range UploadWorkspaceInstanceStatements() {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var instanceID string
	if err := database.QueryRow(`SELECT workspace_instance_id FROM backup_upload_operations WHERE idempotency_key = 'upload-a'`).Scan(&instanceID); err != nil {
		t.Fatal(err)
	}
	if instanceID != "instance-a" {
		t.Fatalf("workspace instance id = %q, want instance-a", instanceID)
	}
}

func TestUploadWorkspaceInstanceStatementsLeaveMissingIdentityFailClosed(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, statement := range []string{
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE backup_upload_operations (idempotency_key TEXT PRIMARY KEY)`,
		`INSERT INTO backup_upload_operations(idempotency_key) VALUES ('upload-a')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range UploadWorkspaceInstanceStatements() {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var instanceID string
	if err := database.QueryRow(`SELECT workspace_instance_id FROM backup_upload_operations WHERE idempotency_key = 'upload-a'`).Scan(&instanceID); err != nil {
		t.Fatal(err)
	}
	if instanceID != "" {
		t.Fatalf("workspace instance id = %q, want fail-closed empty identity", instanceID)
	}
}
