package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/projectcapabilities"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func executeVaultAction(ctx context.Context, server *Server, runtime *databaseRuntime, request vaultrequests.Request) (any, error) {
	var approval vaultApprovalContext
	if err := decodeMap(request.ApprovalContext, &approval); err != nil {
		return nil, err
	}
	if approval.Schema != vaultApprovalContextSchema || approval.TokenID != request.TokenID ||
		approval.ProjectID != request.ProjectID || approval.ActionName != request.ActionName ||
		approval.WorkspaceID != runtime.workspaceUUID || approval.RuntimeInstanceID != runtime.runtimeInstanceID {
		return nil, staleVaultContext("Vault approval context is stale")
	}
	inputHash, err := hashCanonical(request.Input)
	if err != nil || inputHash != approval.InputHash {
		return nil, staleVaultContext("Vault action input changed; send a fresh request")
	}
	hash, err := hashCanonical(approval)
	if err != nil || hash != request.ApprovalContextHash {
		return nil, staleVaultContext("Vault approval context hash is stale")
	}
	capability, err := validateVaultApprovalAuthorization(ctx, server, runtime, request, approval)
	if err != nil {
		return nil, err
	}
	switch request.ActionName {
	case vaultrequests.ActionGenerateItem:
		return executeVaultGenerate(ctx, server, runtime, request, approval)
	case vaultrequests.ActionRestartSession:
		return executeVaultSessionApply(ctx, server, runtime, request, approval, capability)
	default:
		return nil, errors.New("unsupported Vault action")
	}
}

func executeVaultGenerate(
	ctx context.Context,
	server *Server,
	runtime *databaseRuntime,
	request vaultrequests.Request,
	approval vaultApprovalContext,
) (any, error) {
	if server == nil || server.vaultGenerateLimiter == nil ||
		!server.vaultGenerateLimiter.allow(fmt.Sprintf("vault-generate:%s:%d", runtime.id, request.TokenID)) {
		return nil, errors.New("Vault generation rate limit exceeded; wait before generating another item")
	}
	var input vaultGenerateActionInput
	if err := decodeMap(request.Input, &input); err != nil {
		return nil, err
	}
	if input.Name == "" || input.GeneratorKind == "" {
		return nil, errors.New("name and generator_kind are required")
	}
	release, err := runtime.vaultDelivery.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if _, err := validateVaultApprovalAuthorization(ctx, server, runtime, request, approval); err != nil {
		return nil, err
	}
	store, err := projectvault.NewStore(runtime.database, runtime.vault, runtime.workspaceUUID)
	if err != nil {
		return nil, err
	}
	createInput := projectvault.CreateInput{
		Name: input.Name, OwnerProjectID: request.ProjectID, SharedProjectIDs: input.SharedProjectIDs,
		SecretType: input.SecretType, Provider: input.Provider, Environment: input.Environment,
		Description: input.Description, ExpiresAt: input.ExpiresAt,
		ExpiryWarningDays: input.ExpiryWarningDays, Source: "generated",
		GeneratorKind: input.GeneratorKind, Tags: input.Tags, UsageNotes: input.projectUsageNotes(),
	}
	var item projectvault.Item
	err = server.withAuditedMutation(ctx, runtime, "mcp", &request.TokenID, 0, "vault.item.created", func() any {
		return map[string]any{
			"vault_item_id": item.ID, "owner_project_id": item.OwnerProjectID,
			"secret_type": item.SecretType, "source": item.Source,
		}
	}, func(tx *sql.Tx) error {
		var createErr error
		item, createErr = store.WithTx(tx).Create(ctx, createInput)
		return createErr
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"item": map[string]any{
			"vault_ref": "vault:" + strconv.FormatInt(item.ID, 10),
			"item_id":   item.ID, "project_id": item.OwnerProjectID,
			"name": item.Name, "secret_type": item.SecretType, "status": item.Status,
			"expires_at": item.ExpiresAt, "value_version": item.ValueVersion,
			"metadata_revision": item.MetadataRevision,
		},
		"secret_returned": false,
	}, nil
}

func executeVaultSessionApply(
	ctx context.Context,
	server *Server,
	runtime *databaseRuntime,
	request vaultrequests.Request,
	approval vaultApprovalContext,
	capability projectcapabilities.Capability,
) (any, error) {
	var input vaultSessionApplyActionInput
	if err := decodeMap(request.Input, &input); err != nil {
		return nil, err
	}
	snapshot := vaultEnvironmentSnapshotFromApproval(approval)
	if err := validateSnapshotIdentity(snapshot); err != nil {
		return nil, staleVaultContext(err.Error())
	}
	store, err := projectvault.NewStore(runtime.database, runtime.vault, runtime.workspaceUUID)
	if err != nil {
		return nil, err
	}
	local, err := localExecutionPrincipal(runtime)
	if err != nil {
		return nil, err
	}
	principal, err := tokenExecutionPrincipal(runtime, request.TokenID)
	if err != nil {
		return nil, err
	}
	authorize := func(authorizeCtx context.Context) error {
		_, authorizeErr := validateVaultApprovalAuthorization(authorizeCtx, server, runtime, request, approval)
		return authorizeErr
	}
	expiresAt := vaultSessionLeaseExpiry(ctx, runtime, request, approval, capability)
	finalize := func(finalizeCtx context.Context, handle console.SessionHandle) error {
		if err := store.RecordSessionItems(finalizeCtx, handle.ID, approval.Items); err != nil {
			return err
		}
		lease := vaultsessions.Lease{
			WorkspaceID: runtime.workspaceUUID, RuntimeInstanceID: runtime.runtimeInstanceID,
			TokenID: request.TokenID, RuntimeID: handle.RuntimeID, SessionID: handle.ID,
			SessionGeneration: handle.Generation, ApprovalContextHash: request.ApprovalContextHash,
			EnvironmentContentHash: approval.EnvironmentContentHash,
			ExpiresAt:              expiresAt,
			Validate: func(validateCtx context.Context) error {
				revoke := func(message string) error {
					_ = revokePersistedVaultLease(validateCtx, runtime, handle.ID, handle.Generation)
					return errors.New(message)
				}
				if _, err := validateVaultApprovalAuthorization(validateCtx, server, runtime, request, approval); err != nil {
					return revoke("Vault authorization context changed")
				}
				if err := store.RevalidateSession(validateCtx, approval.Items); err != nil {
					return revoke("Vault item context changed")
				}
				return nil
			},
		}
		if err := runtime.vaultLeases.Grant(lease); err != nil {
			return err
		}
		if err := persistVaultLease(finalizeCtx, runtime, request.ProjectID, lease); err != nil {
			runtime.vaultLeases.RevokeSession(handle)
			return err
		}
		if err := store.MarkSessionItemsUsed(finalizeCtx, approval.Items); err != nil {
			runtime.vaultLeases.RevokeSession(handle)
			_ = revokePersistedVaultLease(finalizeCtx, runtime, handle.ID, handle.Generation)
			return err
		}
		return nil
	}
	cols, rows := approval.ExpectedCols, approval.ExpectedRows
	if cols < 1 {
		cols = 120
	}
	if rows < 1 {
		rows = 32
	}
	createRequest := console.CreateRequest{
		RuntimeID: approval.RuntimeID, Name: fmt.Sprintf("Vault session for %s", request.ProjectName),
		CloseExisting: false, Cols: cols, Rows: rows, WaitForStart: true, Principal: principal,
		PrepareEnvironment:     newVaultEnvironmentPreparer(server, runtime, snapshot, input.sessionSelections(), authorize, finalize),
		EnvironmentContentHash: approval.EnvironmentContentHash,
		ApprovalContextHash:    request.ApprovalContextHash,
	}
	expected := console.SessionHandle{}
	if approval.ExpectedSessionID > 0 {
		expected = console.SessionHandle{
			ID: approval.ExpectedSessionID, RuntimeID: approval.RuntimeID, Generation: approval.ExpectedGeneration,
		}
	}
	record, err := runtime.consoleSessions.ReplaceIfCurrent(ctx, local, expected, createRequest)
	if errors.Is(err, console.ErrSessionChanged) || errors.Is(err, console.ErrSessionLimit) {
		return nil, staleVaultContext("session identity changed; send a fresh request")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id": record.ID, "session_generation": record.Generation,
		"runtime_id": record.RuntimeID, "status": record.Status,
		"environment_names": sessionItemNames(approval.Items),
		"expires_at":        expiresAt.Format(time.RFC3339),
	}, nil
}

func vaultSessionLeaseExpiry(
	ctx context.Context,
	runtime *databaseRuntime,
	request vaultrequests.Request,
	approval vaultApprovalContext,
	capability projectcapabilities.Capability,
) time.Time {
	expiresAt := time.Now().UTC().Add(vaultsessions.MaxLeaseTTL)
	if capability.ExpiresAt != "" {
		if parsed, err := time.Parse(time.RFC3339, capability.ExpiresAt); err == nil && parsed.Before(expiresAt) {
			expiresAt = parsed
		}
	}
	if approval.ConnectorPermissionExpiresAt != "" {
		if parsed, err := time.Parse(time.RFC3339, approval.ConnectorPermissionExpiresAt); err == nil && parsed.Before(expiresAt) {
			expiresAt = parsed
		}
	}
	if token, err := runtime.tokens.Get(ctx, request.TokenID); err == nil && token.ExpiresAt != "" {
		if parsed, err := time.Parse(time.RFC3339, token.ExpiresAt); err == nil && parsed.Before(expiresAt) {
			expiresAt = parsed
		}
	}
	return expiresAt
}
