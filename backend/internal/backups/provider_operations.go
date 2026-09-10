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

func PrepareProviderRestore(ctx context.Context, scope HTTPScope, providerID, recordID int64) (PreparedProviderRestore, error) {
	const requirements = requireDatabase | requireDatabasePath | requireSecrets
	if providerID < 1 || recordID < 1 {
		return PreparedProviderRestore{}, ValidationError("backup provider and record ids are required")
	}
	if !scopeSupports(scope, requirements) {
		return PreparedProviderRestore{}, ErrIncompleteScope
	}
	store := NewStore(scope.Database)
	provider, err := store.GetProvider(ctx, providerID)
	if err != nil {
		return PreparedProviderRestore{}, err
	}
	if provider.Status != "active" {
		return PreparedProviderRestore{}, ErrProviderDisabled
	}
	record, err := store.GetRecord(ctx, provider.ID, recordID)
	if err != nil {
		return PreparedProviderRestore{}, err
	}
	client, err := backupServiceClient(scope, provider)
	if err != nil {
		return PreparedProviderRestore{}, err
	}
	path, err := downloadServiceRecordToTemp(ctx, scope, provider, record, client)
	if err != nil {
		return PreparedProviderRestore{}, err
	}
	if path == "" {
		return PreparedProviderRestore{}, fmt.Errorf("backup provider returned an empty restore path")
	}
	return PreparedProviderRestore{
		ProviderID: provider.ID, RecordID: record.ID,
		Filename: record.Filename, SourceMachine: record.SourceMachine, Path: path,
		baseURL: stringFromMap(provider.Public, "base_url"), streamID: stringFromMap(provider.Public, "stream_id"),
		backupID: record.ProviderFileID, backupCreated: record.BackupCreatedAt,
	}, nil
}
