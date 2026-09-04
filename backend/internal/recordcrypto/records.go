package recordcrypto

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/vault"
)

const (
	EnvelopeVersion      = "1"
	markerKey            = "encrypted_record_envelope_version"
	bindingSentinelKey   = "encrypted_record_binding_sentinel"
	bindingSentinelValue = "aipermission-record-storage-binding-v1"
)

type RecordType struct {
	Table  string
	Column string
	Domain string
}

var (
	ConnectorCredentialProfile  = RecordType{Table: "connector_credential_profiles", Column: "encrypted_secret_json", Domain: "connector-credential-profile"}
	ConnectorCredentialResource = RecordType{Table: "connector_credential_resources", Column: "encrypted_secret", Domain: "connector-credential-resource"}
	APIToken                    = RecordType{Table: "api_tokens", Column: "token_value", Domain: "api-token"}
	ConnectorActionRequest      = RecordType{Table: "connector_action_requests", Column: "encrypted_payload_json", Domain: "connector-action-request"}
	CommandRequest              = RecordType{Table: "command_requests", Column: "encrypted_command", Domain: "command-request"}
	BackupProvider              = RecordType{Table: "backup_providers", Column: "encrypted_secret_json", Domain: "backup-provider"}
)

var persistentRecordTypes = []RecordType{
	ConnectorCredentialProfile,
	ConnectorCredentialResource,
	APIToken,
	ConnectorActionRequest,
	CommandRequest,
	BackupProvider,
}

// PersistentRecordTypes returns the storage fields that must use record-bound
// envelopes. Database migrations use the same catalog to install write guards.
func PersistentRecordTypes() []RecordType {
	return append([]RecordType(nil), persistentRecordTypes...)
}

type RewriteStats struct {
	Rewritten int
	Verified  int
}

func Context(workspaceID string, recordType RecordType, recordID int64) vault.RecordContext {
	return vault.RecordContext{
		WorkspaceID: workspaceID,
		Domain:      recordType.Domain,
		RecordID:    strconv.FormatInt(recordID, 10),
		Field:       recordType.Column,
	}
}

func EncryptJSON(secretVault *vault.Vault, workspaceID string, recordType RecordType, recordID int64, value any) (string, error) {
	if err := validateRecordType(recordType); err != nil {
		return "", err
	}
	return secretVault.EncryptRecordJSON(value, Context(workspaceID, recordType, recordID))
}

func DecryptJSON(secretVault *vault.Vault, workspaceID string, recordType RecordType, recordID int64, encrypted string, target any) error {
	if err := validateRecordType(recordType); err != nil {
		return err
	}
	return secretVault.DecryptRecordJSON(encrypted, target, Context(workspaceID, recordType, recordID))
}

func EnvelopeMarkerPresent(ctx context.Context, database *sql.DB) (bool, error) {
	var marker string
	err := database.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, markerKey).Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read encrypted record migration marker: %w", err)
	}
	if marker != EnvelopeVersion {
		return false, fmt.Errorf("unsupported encrypted record migration version %q", marker)
	}
	return true, nil
}

// RewriteLegacy rewrites every legacy encrypted database field atomically.
// The completion marker is committed only after every legacy payload has been
// authenticated and every current envelope has been verified in place.
func RewriteLegacy(ctx context.Context, database *sql.DB, secretVault *vault.Vault, workspaceID string) (RewriteStats, error) {
	if database == nil || secretVault == nil || strings.TrimSpace(workspaceID) == "" {
		return RewriteStats{}, fmt.Errorf("encrypted record migration requires database, vault, and workspace ID")
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return RewriteStats{}, fmt.Errorf("begin encrypted record migration: %w", err)
	}
	defer tx.Rollback()

	var marker string
	err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, markerKey).Scan(&marker)
	if err != nil && err != sql.ErrNoRows {
		return RewriteStats{}, fmt.Errorf("read encrypted record migration marker: %w", err)
	}
	markerPresent := err == nil
	if markerPresent && marker != EnvelopeVersion {
		return RewriteStats{}, fmt.Errorf("unsupported encrypted record migration version %q", marker)
	}
	var sentinel string
	sentinelErr := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, bindingSentinelKey).Scan(&sentinel)
	if sentinelErr != nil && !errors.Is(sentinelErr, sql.ErrNoRows) {
		return RewriteStats{}, fmt.Errorf("read encrypted record binding sentinel: %w", sentinelErr)
	}
	if markerPresent && sentinelErr == nil {
		if err := verifyBindingSentinel(secretVault, workspaceID, sentinel); err != nil {
			return RewriteStats{}, err
		}
		return RewriteStats{}, nil
	}

	stats := RewriteStats{}
	for _, recordType := range persistentRecordTypes {
		var lastID *int64
		for {
			record, found, err := loadNextEncryptedRecord(ctx, tx, recordType, lastID)
			if err != nil {
				return RewriteStats{}, err
			}
			if !found {
				break
			}
			lastID = &record.id
			context := Context(workspaceID, recordType, record.id)
			var plain json.RawMessage
			legacy, err := secretVault.DecryptRecordJSONWithLegacy(record.encrypted, &plain, context, nil)
			if err != nil {
				return RewriteStats{}, fmt.Errorf("verify %s record %d: %w", recordType.Domain, record.id, err)
			}
			if !legacy || markerPresent {
				stats.Verified++
				continue
			}
			rewritten, err := secretVault.EncryptRecordJSON(plain, context)
			if err != nil {
				return RewriteStats{}, fmt.Errorf("rewrite %s record %d: %w", recordType.Domain, record.id, err)
			}
			query := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE id = ?`, recordType.Table, recordType.Column)
			result, err := tx.ExecContext(ctx, query, rewritten, record.id)
			if err != nil {
				return RewriteStats{}, fmt.Errorf("store rewritten %s record %d: %w", recordType.Domain, record.id, err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return RewriteStats{}, fmt.Errorf("check rewritten %s record %d: %w", recordType.Domain, record.id, err)
			}
			if affected != 1 {
				return RewriteStats{}, fmt.Errorf("rewrite %s record %d affected %d rows", recordType.Domain, record.id, affected)
			}
			stats.Rewritten++
		}
	}
	sentinel, err = secretVault.EncryptRecordJSON(bindingSentinelValue, bindingContext(workspaceID))
	if err != nil {
		return RewriteStats{}, fmt.Errorf("encrypt record binding sentinel: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, bindingSentinelKey, sentinel); err != nil {
		return RewriteStats{}, fmt.Errorf("write encrypted record binding sentinel: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, markerKey, EnvelopeVersion); err != nil {
		return RewriteStats{}, fmt.Errorf("write encrypted record migration marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RewriteStats{}, fmt.Errorf("commit encrypted record migration: %w", err)
	}
	return stats, nil
}

type encryptedRecord struct {
	id        int64
	encrypted string
}

func loadNextEncryptedRecord(ctx context.Context, tx *sql.Tx, recordType RecordType, afterID *int64) (encryptedRecord, bool, error) {
	if err := validateRecordType(recordType); err != nil {
		return encryptedRecord{}, false, err
	}
	query := fmt.Sprintf(`SELECT id, %s FROM %s WHERE %s <> ''`, recordType.Column, recordType.Table, recordType.Column)
	args := []any{}
	if afterID != nil {
		query += ` AND id > ?`
		args = append(args, *afterID)
	}
	query += ` ORDER BY id LIMIT 1`
	var record encryptedRecord
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&record.id, &record.encrypted); err != nil {
		if err == sql.ErrNoRows {
			return encryptedRecord{}, false, nil
		}
		return encryptedRecord{}, false, fmt.Errorf("read next %s encrypted record: %w", recordType.Domain, err)
	}
	return record, true, nil
}

func bindingContext(workspaceID string) vault.RecordContext {
	return vault.RecordContext{
		WorkspaceID: workspaceID,
		Domain:      "record-storage-binding",
		RecordID:    "1",
		Field:       bindingSentinelKey,
	}
}

func verifyBindingSentinel(secretVault *vault.Vault, workspaceID, encrypted string) error {
	var value string
	if err := secretVault.DecryptRecordJSON(encrypted, &value, bindingContext(workspaceID)); err != nil {
		return fmt.Errorf("verify encrypted record storage binding: %w", err)
	}
	if value != bindingSentinelValue {
		return fmt.Errorf("verify encrypted record storage binding: sentinel mismatch")
	}
	return nil
}

func validateRecordType(recordType RecordType) error {
	for _, known := range persistentRecordTypes {
		if recordType == known {
			return nil
		}
	}
	return fmt.Errorf("unknown encrypted record type")
}
