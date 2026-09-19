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
)

type providerSecretCodec struct{ runtime Runtime }

func (codec providerSecretCodec) EncryptProviderSecret(id int64, secret map[string]any) (string, error) {
	return recordcrypto.EncryptJSON(codec.runtime.SecretVault, codec.runtime.WorkspaceID, recordcrypto.BackupProvider, id, secret)
}

func (codec providerSecretCodec) DecryptProviderSecret(provider backups.Provider) (map[string]any, error) {
	if provider.EncryptedSecretJSON == "" {
		return map[string]any{}, nil
	}
	secret := map[string]any{}
	err := recordcrypto.DecryptJSON(codec.runtime.SecretVault, codec.runtime.WorkspaceID, recordcrypto.BackupProvider, provider.ID, provider.EncryptedSecretJSON, &secret)
	return secret, err
}

func (component *Component) providerScope(w http.ResponseWriter) (backups.HTTPScope, bool) {
	runtime, ok := component.dependencies.ActiveRuntime(w)
	if !ok {
		return backups.HTTPScope{}, false
	}
	return backups.HTTPScope{
		Database: runtime.Database, DatabaseID: runtime.DatabaseID,
		DatabaseName: component.dependencies.CurrentDatabaseName(), DatabasePath: runtime.DatabasePath,
		WorkspaceUUID: runtime.WorkspaceID, InstallationDataPath: component.dependencies.DataPath,
		Secrets: providerSecretCodec{runtime: runtime},
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return runtime.Mutate(ctx, action, payload, mutate)
		},
		AuditRequired: func(ctx context.Context, action string, payload any) error {
			return runtime.AuditRequired(ctx, action, payload)
		},
		Observe: func(ctx context.Context, action string, payload any) {
			runtime.Observe(ctx, action, payload)
		},
		CreateSnapshot: func(ctx context.Context) (backups.DatabaseSnapshot, error) {
			snapshot, err := backups.CreateDatabaseSnapshot(ctx, backups.SnapshotSource{Database: runtime.Database, DatabaseID: runtime.DatabaseID, Path: runtime.DatabasePath})
			return backups.DatabaseSnapshot{Path: snapshot.Path}, err
		},
		AuthorizePassword: func(response http.ResponseWriter, request *http.Request, password string) bool {
			attempt, ok := component.dependencies.BeginAttempt(response, request)
			if !ok {
				return false
			}
			if err := dbpkg.ValidateEncrypted(runtime.DatabasePath, password); err != nil {
				attempt.Failure()
				httptransport.WriteError(response, http.StatusUnauthorized, "invalid current database password")
				return false
			}
			attempt.Success()
			return true
		},
	}, true
}

func (component *Component) providerOperationScope(w http.ResponseWriter, r *http.Request) (backups.HTTPScope, func(), bool) {
	lease, ok := component.authorizedReadOperation(w, r)
	if !ok {
		return backups.HTTPScope{}, nil, false
	}
	scope, ok := component.providerScope(w)
	if !ok {
		lease.Release()
		return backups.HTTPScope{}, nil, false
	}
	return scope, lease.Release, true
}

type restoreProviderRequest struct {
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

func (component *Component) restoreProviderRecord(w http.ResponseWriter, r *http.Request) {
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
	lease, ok := component.authorizedMutationOperation(w, r)
	if !ok {
		return
	}
	defer lease.Release()
	scope, ok := component.providerScope(w)
	if !ok {
		return
	}
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
	component.installImportedDatabase(w, r, request.DatabaseName, request.DatabasePassword, backups.CopyBackupFile(prepared.Path), func(database *sql.DB) error {
		return prepared.RecordBaseline(r.Context(), database)
	})
}
