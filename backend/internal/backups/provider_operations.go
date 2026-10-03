package backups

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

var ErrIncompleteScope = errors.New("backup provider scope is incomplete")
var ErrProviderDisabled = errors.New("backup provider is disabled")

type remoteOperationError struct{ err error }

func (err remoteOperationError) Error() string { return err.err.Error() }
func (err remoteOperationError) Unwrap() error { return err.err }

func EnableProvider(ctx context.Context, scope HTTPScope, id int64) (Provider, error) {
	const requirements = requireDatabase | requireSecrets | requireMutation
	if id < 1 {
		return Provider{}, ValidationError("backup provider id is required")
	}
	if !scopeSupports(scope, requirements) {
		return Provider{}, ErrIncompleteScope
	}
	store := NewStore(scope.Database)
	provider, err := store.GetProvider(ctx, id)
	if err != nil {
		return Provider{}, err
	}
	client, err := backupServiceClient(scope, provider)
	if err != nil {
		return Provider{}, err
	}
	info, err := client.Info(ctx)
	if err != nil {
		return Provider{}, remoteOperationError{err: err}
	}
	public := cloneJSONMap(provider.Public)
	public["service_version"] = info.Version
	public["protocol_version"] = info.ProtocolVersion
	var item Provider
	err = scope.Mutate(ctx, "backup.provider.enabled", func() any {
		payload := backupProviderAuditPayload(item)
		payload["service_version"] = info.Version
		return payload
	}, func(tx *sql.Tx) error {
		txStore := NewTxStore(tx)
		var updateErr error
		item, updateErr = txStore.UpdateProvider(ctx, id, UpdateProviderRequest{
			Name: provider.Name, Status: "active", Public: public,
		})
		if updateErr != nil {
			return updateErr
		}
		if updateErr = txStore.UpdateLastChecked(ctx, id, time.Now()); updateErr != nil {
			return updateErr
		}
		item, updateErr = txStore.GetProvider(ctx, id)
		return updateErr
	})
	return item, err
}

type PreparedProviderRestore struct {
	ProviderID    int64
	RecordID      int64
	Filename      string
	SourceMachine string
	Path          string
	baseURL       string
	streamID      string
	backupID      string
	backupCreated string
}

func (prepared PreparedProviderRestore) Remove() {
	if prepared.Path != "" {
		_ = os.Remove(prepared.Path)
	}
}

// RecordBaseline stores the restored remote version in the newly installed
// database without exposing provider-specific metadata to the composition root.
func (prepared PreparedProviderRestore) RecordBaseline(ctx context.Context, database *sql.DB) error {
	if database == nil || prepared.baseURL == "" || prepared.streamID == "" || prepared.backupID == "" {
		return ErrIncompleteScope
	}
	return WriteServiceBaseline(ctx, database, prepared.baseURL, prepared.streamID, ServiceBackup{
		ID: prepared.backupID, CreatedAt: prepared.backupCreated,
	})
}

// ProviderRestoreSelection is detached from the database and Vault after
// selection. Download uses only immutable metadata and a captured client.
type ProviderRestoreSelection struct {
	info         PreparedProviderRestore
	provider     Provider
	record       Record
	client       *ServiceClient
	databasePath string
}

func (selection ProviderRestoreSelection) Info() PreparedProviderRestore { return selection.info }

func (selection ProviderRestoreSelection) Current(ctx context.Context, database *sql.DB) (bool, error) {
	if database == nil {
		return false, ErrIncompleteScope
	}
	public, err := marshalJSONObject(selection.provider.Public)
	if err != nil {
		return false, err
	}
	var current bool
	err = database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM backup_providers p
		JOIN backup_records r ON r.provider_id = p.id WHERE p.id = ? AND p.status = 'active'
		AND p.provider_type = ? AND p.public_json = ? AND p.encrypted_secret_json = ?
		AND r.id = ? AND r.deleted_at IS NULL AND r.provider_file_id = ? AND r.size_bytes = ? AND r.checksum_sha256 = ?)`,
		selection.provider.ID, selection.provider.ProviderType, public, selection.provider.EncryptedSecretJSON,
		selection.record.ID, selection.record.ProviderFileID, selection.record.SizeBytes, selection.record.ChecksumSHA256).Scan(&current)
	return current, err
}

func SelectProviderRestore(ctx context.Context, scope HTTPScope, providerID, recordID int64) (ProviderRestoreSelection, error) {
	const requirements = requireDatabase | requireDatabasePath | requireSecrets
	if providerID < 1 || recordID < 1 {
		return ProviderRestoreSelection{}, ValidationError("backup provider and record ids are required")
	}
	if !scopeSupports(scope, requirements) {
		return ProviderRestoreSelection{}, ErrIncompleteScope
	}
	store := NewStore(scope.Database)
	provider, err := store.GetProvider(ctx, providerID)
	if err != nil {
		return ProviderRestoreSelection{}, err
	}
	if provider.Status != "active" {
		return ProviderRestoreSelection{}, ErrProviderDisabled
	}
	record, err := store.GetRecord(ctx, provider.ID, recordID)
	if err != nil {
		return ProviderRestoreSelection{}, err
	}
	client, err := backupServiceClient(scope, provider)
	if err != nil {
		return ProviderRestoreSelection{}, err
	}
	return ProviderRestoreSelection{
		provider: provider, record: record, client: client, databasePath: scope.DatabasePath,
		info: PreparedProviderRestore{
			ProviderID: provider.ID, RecordID: record.ID, Filename: record.Filename, SourceMachine: record.SourceMachine,
			baseURL: stringFromMap(provider.Public, "base_url"), streamID: stringFromMap(provider.Public, "stream_id"),
			backupID: record.ProviderFileID, backupCreated: record.BackupCreatedAt,
		},
	}, nil
}

func (selection ProviderRestoreSelection) Download(ctx context.Context) (PreparedProviderRestore, error) {
	if selection.client == nil || selection.databasePath == "" {
		return PreparedProviderRestore{}, ErrIncompleteScope
	}
	if selection.record.SizeBytes < 1 || selection.record.SizeBytes > MaxDatabaseTransferBytes {
		return PreparedProviderRestore{}, ValidationError("backup is too large to download through the gateway")
	}
	path, err := selection.client.downloadVerifiedTemp(ctx, selection.databasePath, fmt.Sprintf("remote-backup-%d-*.aipdb", selection.provider.ID),
		stringFromMap(selection.provider.Public, "stream_id"), ServiceBackup{ID: selection.record.ProviderFileID, SizeBytes: selection.record.SizeBytes, SHA256: selection.record.ChecksumSHA256})
	if err != nil {
		return PreparedProviderRestore{}, remoteOperationError{err: err}
	}
	prepared := selection.info
	prepared.Path = path
	return prepared, nil
}
