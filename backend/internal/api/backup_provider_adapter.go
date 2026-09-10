package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/backups"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

type backupProviderSecretCodec struct{ runtime *databaseRuntime }

func (codec backupProviderSecretCodec) EncryptProviderSecret(providerID int64, secret map[string]any) (string, error) {
	return recordcrypto.EncryptJSON(
		codec.runtime.vault, codec.runtime.workspaceUUID,
		recordcrypto.BackupProvider, providerID, secret,
	)
}

func (codec backupProviderSecretCodec) DecryptProviderSecret(provider backups.Provider) (map[string]any, error) {
	if provider.EncryptedSecretJSON == "" {
		return map[string]any{}, nil
	}
	secret := map[string]any{}
	err := recordcrypto.DecryptJSON(
		codec.runtime.vault, codec.runtime.workspaceUUID,
		recordcrypto.BackupProvider, provider.ID, provider.EncryptedSecretJSON, &secret,
	)
	return secret, err
}

func (s *Server) backupProviderHTTPScope(w http.ResponseWriter) (backups.HTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return backups.HTTPScope{}, false
	}
	s.mu.RLock()
	databaseName := s.currentDatabaseNameLocked()
	s.mu.RUnlock()
	return backups.HTTPScope{
		Database: runtime.database, DatabaseID: runtime.id, DatabaseName: databaseName,
		DatabasePath: runtime.path, WorkspaceUUID: runtime.workspaceUUID,
		InstallationDataPath: s.config.DataPath,
		Secrets:              backupProviderSecretCodec{runtime: runtime},
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
		AuditRequired: func(ctx context.Context, action string, payload any) error {
			return s.writeAuditRequired(ctx, runtime, "user", nil, 0, action, payload)
		},
		Observe: func(ctx context.Context, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
		},
		AcquireOperation: s.backupOperations.Acquire,
		CreateSnapshot: func(ctx context.Context) (backups.DatabaseSnapshot, error) {
			snapshot, err := backups.CreateDatabaseSnapshot(ctx, backups.SnapshotSource{
				Database: runtime.database, DatabaseID: runtime.id, Path: runtime.path,
			})
			return backups.DatabaseSnapshot{Path: snapshot.Path}, err
		},
		AuthorizePassword: func(response http.ResponseWriter, request *http.Request, password string) bool {
			attempt, ok := s.beginDatabasePasswordAttempt(response, request)
			if !ok {
				return false
			}
			if err := dbpkg.ValidateEncrypted(runtime.path, password); err != nil {
				attempt.failure()
				writeError(response, http.StatusUnauthorized, "invalid current database password")
				return false
			}
			attempt.success()
			return true
		},
	}, true
}

type restoreBackupRecordRequest struct {
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

func (s backupHandlers) restoreProviderRecord(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.backupProviderHTTPScope(w)
	if !ok {
		return
	}
	providerID, ok := parseID(w, r)
	if !ok {
		return
	}
	recordID, ok := parsePathInt64(w, r, "record_id", "backup record id")
	if !ok {
		return
	}
	var request restoreBackupRecordRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	defer clearStringReferences(&request.DatabasePassword)
	release, err := scope.AcquireOperation(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "backup restore was canceled")
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
		"provider_id": prepared.ProviderID, "record_id": prepared.RecordID,
		"filename": prepared.Filename, "database_name": strings.TrimSpace(request.DatabaseName),
		"source_machine": prepared.SourceMachine,
	})
	s.installImportedDatabaseWithMutator(
		w, r, request.DatabaseName, request.DatabasePassword,
		backups.CopyBackupFile(prepared.Path),
		func(database *sql.DB) error { return prepared.RecordBaseline(r.Context(), database) },
	)
}
