package api

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"github.com/aipermission/aipermission/backend/internal/auditoutbox"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func (s *Server) vaultRequestStore(ctx context.Context, runtime *databaseRuntime) *vaultrequests.Store {
	redact := s.prepareAuditRedactor(ctx, runtime)
	return vaultrequests.NewStore(runtime.database).WithMutationHook(func(ctx context.Context, executor sqldb.Executor, item vaultrequests.Request) error {
		event, err := s.buildAuditEventWithRedactor(
			ctx, executor, "gateway", int64Ptr(item.TokenID), valueOrZero(item.RuntimeID),
			"vault.action_request."+item.Status, vaultActionAuditPayload(item, item.UserNote), redact,
		)
		if err != nil {
			return err
		}
		_, err = (auditoutbox.Store{}).Append(ctx, executor, event)
		return err
	})
}

func (s *Server) runVaultActionRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	requestID int64,
	actor string,
	userNote string,
	startedAction string,
	finishedActionPrefix string,
) (vaultrequests.WorkflowResult, error) {
	return vaultrequests.RunWorkflow(ctx, requestID, s.vaultActionWorkflowPorts(runtime, actor, userNote, startedAction, finishedActionPrefix))
}

func (s *Server) vaultActionWorkflowPorts(runtime *databaseRuntime, actor, userNote, startedAction, finishedActionPrefix string) vaultrequests.WorkflowPorts {
	return vaultrequests.WorkflowPorts{
		Claim: func(ctx context.Context, id int64) (vaultrequests.Request, error) {
			return s.claimVaultActionRequest(ctx, runtime, id, actor, startedAction, userNote)
		},
		Execute: func(ctx context.Context, item vaultrequests.Request) (any, error) {
			return executeVaultAction(ctx, s, runtime, item)
		},
		Complete: func(ctx context.Context, id int64, status string, output any, errorText string) (vaultrequests.Request, error) {
			return s.completeVaultActionRequest(ctx, runtime, id, status, output, errorText, userNote, actor, finishedActionPrefix+"."+status)
		},
		Get: func(ctx context.Context, id int64) (vaultrequests.Request, error) {
			return s.vaultRequestStore(ctx, runtime).Get(ctx, id)
		},
		Repair: func(ctx context.Context, id int64) error {
			if err := history.NewStore(runtime.database).SyncVaultActionRequest(ctx, id); err != nil {
				log.Printf("Vault request history projection repair failed request=%d error=%v", id, err)
			}
			return nil
		},
		Compensate: func(ctx context.Context, item vaultrequests.Request, output any) error {
			return compensateVaultActionEffect(ctx, runtime, item, output)
		},
		RedactError: func(ctx context.Context, err error) string {
			return s.redactForPersistence(ctx, runtime, err.Error())
		},
		IsStale: isVaultContextDrift,
	}
}

func (s *Server) claimVaultActionRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	requestID int64,
	actor string,
	action string,
	userNote string,
) (vaultrequests.Request, error) {
	tokenID, runtimeID := vaultActionAuditIdentity(ctx, runtime, requestID)
	var item vaultrequests.Request
	err := s.withAuditedMutation(
		ctx, runtime, actor, tokenID, runtimeID, action,
		func() any { return vaultActionAuditPayload(item, userNote) },
		func(tx *sql.Tx) error {
			var err error
			item, err = vaultrequests.NewTxStore(tx).Claim(ctx, requestID)
			return err
		},
	)
	return item, err
}

func (s *Server) completeVaultActionRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	requestID int64,
	status string,
	output any,
	errorText string,
	userNote string,
	actor string,
	action string,
) (vaultrequests.Request, error) {
	tokenID, runtimeID := vaultActionAuditIdentity(ctx, runtime, requestID)
	var item vaultrequests.Request
	err := s.withAuditedMutation(
		ctx, runtime, actor, tokenID, runtimeID, action,
		func() any { return vaultActionAuditPayload(item, userNote) },
		func(tx *sql.Tx) error {
			var err error
			item, err = vaultrequests.NewTxStore(tx).Complete(ctx, requestID, status, output, errorText, userNote)
			return err
		},
	)
	return item, err
}

func vaultActionAuditIdentity(ctx context.Context, runtime *databaseRuntime, requestID int64) (*int64, int64) {
	if runtime == nil || runtime.database == nil || requestID < 1 {
		return nil, 0
	}
	var tokenID int64
	var runtimeID sql.NullInt64
	if err := runtime.database.QueryRowContext(ctx, `
		SELECT token_id, runtime_id FROM vault_action_requests WHERE id = ?`, requestID,
	).Scan(&tokenID, &runtimeID); err != nil {
		log.Printf("resolve Vault action audit identity request_id=%d error=%v", requestID, err)
		return nil, 0
	}
	return &tokenID, runtimeID.Int64
}

func compensateVaultActionEffect(ctx context.Context, runtime *databaseRuntime, request vaultrequests.Request, output any) error {
	payload, _ := output.(map[string]any)
	switch request.ActionName {
	case vaultrequests.ActionRestartSession:
		sessionID := vaultJSONInt(payload["session_id"])
		if sessionID < 1 {
			return nil
		}
		runtimeID := vaultJSONInt(payload["runtime_id"])
		generation := vaultJSONInt(payload["session_generation"])
		if runtimeID > 0 && generation > 0 {
			runtime.vaultLeases.RevokeSession(console.SessionHandle{
				ID: sessionID, RuntimeID: runtimeID, Generation: generation,
			})
		}
		var cleanupErrors []error
		if generation > 0 {
			if err := revokePersistedVaultLease(ctx, runtime, sessionID, generation); err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
		}
		principal, err := localExecutionPrincipal(runtime)
		if err != nil {
			return err
		}
		if err := runtime.consoleSessions.Close(ctx, principal, sessionID); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
		return errors.Join(cleanupErrors...)
	case vaultrequests.ActionGenerateItem:
		itemPayload, _ := payload["item"].(map[string]any)
		itemID := vaultJSONInt(itemPayload["item_id"])
		valueVersion := vaultJSONInt(itemPayload["value_version"])
		metadataRevision := vaultJSONInt(itemPayload["metadata_revision"])
		if itemID < 1 || valueVersion < 1 || metadataRevision < 1 {
			return nil
		}
		release, err := runtime.vaultDelivery.acquireExclusive(ctx)
		if err != nil {
			return err
		}
		defer release()
		store, err := projectvault.NewStore(runtime.database, runtime.vault, runtime.workspaceUUID)
		if err != nil {
			return err
		}
		return store.Delete(ctx, itemID, valueVersion, metadataRevision)
	default:
		return nil
	}
}
