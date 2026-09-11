package backup

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/backups"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type providerSecretCodec struct{ runtime workspaceruntime.Port }

func (codec providerSecretCodec) EncryptProviderSecret(id int64, secret map[string]any) (string, error) {
	return recordcrypto.EncryptJSON(codec.runtime.StoragePort().SecretVault(), codec.runtime.WorkspaceIdentifier(), recordcrypto.BackupProvider, id, secret)
}

func (codec providerSecretCodec) DecryptProviderSecret(provider backups.Provider) (map[string]any, error) {
	if provider.EncryptedSecretJSON == "" {
		return map[string]any{}, nil
	}
	secret := map[string]any{}
	err := recordcrypto.DecryptJSON(codec.runtime.StoragePort().SecretVault(), codec.runtime.WorkspaceIdentifier(), recordcrypto.BackupProvider, provider.ID, provider.EncryptedSecretJSON, &secret)
	return secret, err
}

func (component *Component) providerScope(w http.ResponseWriter) (backups.HTTPScope, bool) {
	runtime, ok := component.dependencies.ActiveRuntime(w)
	if !ok {
		return backups.HTTPScope{}, false
	}
	return backups.HTTPScope{
		Database: runtime.StoragePort().DatabaseHandle(), DatabaseID: runtime.DatabaseIdentifier(),
		DatabaseName: component.dependencies.CurrentDatabaseName(), DatabasePath: runtime.DatabasePath(),
		WorkspaceUUID: runtime.WorkspaceIdentifier(), InstallationDataPath: component.dependencies.DataPath,
		Secrets: providerSecretCodec{runtime: runtime}, AcquireOperation: component.dependencies.AcquireOperation,
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return component.dependencies.Mutate(ctx, runtime, action, payload, mutate)
		},
		AuditRequired: func(ctx context.Context, action string, payload any) error {
			return component.dependencies.AuditRequired(ctx, runtime, action, payload)
		},
		Observe: func(ctx context.Context, action string, payload any) {
			component.dependencies.Observe(ctx, runtime, action, payload)
		},
		CreateSnapshot: func(ctx context.Context) (backups.DatabaseSnapshot, error) {
			snapshot, err := backups.CreateDatabaseSnapshot(ctx, backups.SnapshotSource{Database: runtime.StoragePort().DatabaseHandle(), DatabaseID: runtime.DatabaseIdentifier(), Path: runtime.DatabasePath()})
			return backups.DatabaseSnapshot{Path: snapshot.Path}, err
		},
		AuthorizePassword: func(response http.ResponseWriter, request *http.Request, password string) bool {
			attempt, ok := component.dependencies.BeginAttempt(response, request)
			if !ok {
				return false
			}
			if err := dbpkg.ValidateEncrypted(runtime.DatabasePath(), password); err != nil {
				attempt.Failure()
				httptransport.WriteError(response, http.StatusUnauthorized, "invalid current database password")
				return false
			}
			attempt.Success()
			return true
		},
	}, true
}

type restoreProviderRequest struct {
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

func (component *Component) restoreProviderRecord(w http.ResponseWriter, r *http.Request) {
	scope, ok := component.providerScope(w)
	if !ok {
		return
	}
	providerID, ok := parsePositivePathID(w, r, "id", "id")
	if !ok {
		return
	}
	recordID, ok := parsePositivePathID(w, r, "record_id", "backup record id")
	if !ok {
		return
	}
	var request restoreProviderRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	defer clearStrings(&request.DatabasePassword)
	release, err := scope.AcquireOperation(r.Context())
	if err != nil {
		httptransport.WriteError(w, http.StatusRequestTimeout, "backup restore was canceled")
		return
	}
	defer release()
	prepared, err := backups.PrepareProviderRestore(r.Context(), scope, providerID, recordID)
	if err != nil {
		backups.WriteProviderHTTPError(w, err)
		return
	}
	defer prepared.Remove()
	scope.Observe(r.Context(), "backup.provider.record.restore_requested", map[string]any{
		"provider_id": prepared.ProviderID, "record_id": prepared.RecordID, "filename": prepared.Filename,
		"database_name": strings.TrimSpace(request.DatabaseName), "source_machine": prepared.SourceMachine,
	})
	component.InstallImportedDatabase(w, r, request.DatabaseName, request.DatabasePassword, backups.CopyBackupFile(prepared.Path), func(database *sql.DB) error {
		return prepared.RecordBaseline(r.Context(), database)
	})
}
