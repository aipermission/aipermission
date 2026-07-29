package api

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type vaultActionRunResult struct {
	Request        vaultrequests.Request
	ExecutionError error
}

func (s *Server) runVaultActionRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	requestID int64,
	actor string,
	userNote string,
	startedAction string,
	finishedActionPrefix string,
) (vaultActionRunResult, error) {
	store := vaultrequests.NewStore(runtime.database)
	item, err := store.Claim(ctx, requestID)
	if err != nil {
		return vaultActionRunResult{}, err
	}
	return s.executeClaimedVaultActionRequest(
		ctx,
		runtime,
		item,
		actor,
		userNote,
		startedAction,
		finishedActionPrefix,
	)
}

func (s *Server) executeClaimedVaultActionRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	item vaultrequests.Request,
	actor string,
	userNote string,
	startedAction string,
	finishedActionPrefix string,
) (vaultActionRunResult, error) {
	store := vaultrequests.NewStore(runtime.database)
	if err := s.writeAuditRequired(
		ctx,
		runtime,
		actor,
		int64Ptr(item.TokenID),
		valueOrZero(item.RuntimeID),
		startedAction,
		vaultActionAuditPayload(item, userNote),
	); err != nil {
		_, _ = store.Complete(ctx, item.ID, vaultrequests.StatusFailed, nil, "audit write failed before execution", userNote)
		return vaultActionRunResult{}, err
	}

	output, executeErr := executeVaultAction(ctx, s, runtime, item)
	status := vaultrequests.StatusCompleted
	errorText := ""
	if executeErr != nil {
		status = vaultrequests.StatusFailed
		errorText = s.redactForPersistence(ctx, runtime, executeErr.Error())
		if isVaultContextDrift(executeErr) {
			status = vaultrequests.StatusStale
		}
	}
	completed, err := store.Complete(ctx, item.ID, status, output, errorText, userNote)
	if err != nil {
		current, getErr := store.Get(ctx, item.ID)
		if getErr == nil && current.Status == status {
			if repairErr := history.NewStore(runtime.database).SyncVaultActionRequest(ctx, item.ID); repairErr != nil {
				log.Printf("Vault request history projection repair failed request=%d error=%v", item.ID, repairErr)
			}
			completed = current
		} else {
			if compensateErr := compensateVaultActionEffect(ctx, runtime, item, output); compensateErr != nil {
				return vaultActionRunResult{}, fmt.Errorf("finalize Vault action: %w; compensate effect: %v", err, compensateErr)
			}
			failure := "Vault action effect was rolled back because request finalization failed"
			failed, failErr := store.Complete(ctx, item.ID, vaultrequests.StatusFailed, nil, failure, userNote)
			if failErr != nil {
				return vaultActionRunResult{}, fmt.Errorf("finalize Vault action: %w; record compensation: %v", err, failErr)
			}
			return vaultActionRunResult{Request: failed, ExecutionError: fmt.Errorf("%s", failure)}, nil
		}
	}
	item = completed
	if err := s.writeAuditRequired(
		ctx,
		runtime,
		actor,
		int64Ptr(item.TokenID),
		valueOrZero(item.RuntimeID),
		finishedActionPrefix+"."+status,
		vaultActionAuditPayload(item, userNote),
	); err != nil {
		log.Printf("Vault terminal audit write failed request=%d status=%s error=%v", item.ID, status, err)
	}
	return vaultActionRunResult{Request: item, ExecutionError: executeErr}, nil
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
		release, err := runtime.vaultDelivery.acquire(ctx)
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
