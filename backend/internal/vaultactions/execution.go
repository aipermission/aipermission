package vaultactions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func (r *Runtime) Execute(ctx context.Context, request vaultrequests.Request) (any, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	approval, capability, err := r.authorizeRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	switch request.ActionName {
	case vaultrequests.ActionGenerateItem:
		return nil, errors.New("Vault item generation requires transactional request finalization")
	case vaultrequests.ActionRestartSession:
		return r.executeSessionApply(ctx, request, approval, capability)
	default:
		return nil, errors.New("unsupported Vault action")
	}
}

func (r *Runtime) authorizeRequest(ctx context.Context, request vaultrequests.Request) (vaultrequests.ApprovalContext, accesscontrol.Capability, error) {
	approval, err := vaultrequests.DecodeApprovalContext(request.ApprovalContext)
	if err != nil {
		return vaultrequests.ApprovalContext{}, accesscontrol.Capability{}, err
	}
	if approval.Schema != vaultrequests.ApprovalContextSchema || approval.TokenID != request.TokenID ||
		approval.ProjectID != request.ProjectID || approval.ActionName != request.ActionName ||
		approval.WorkspaceID != r.workspaceID || approval.RuntimeInstanceID != r.runtimeInstanceID {
		return vaultrequests.ApprovalContext{}, accesscontrol.Capability{}, staleContext("Vault approval context is stale")
	}
	inputHash, err := hashCanonical(request.Input)
	if err != nil || inputHash != approval.InputHash {
		return vaultrequests.ApprovalContext{}, accesscontrol.Capability{}, staleContext("Vault action input changed; send a fresh request")
	}
	hash, err := hashCanonical(approval)
	if err != nil || hash != request.ApprovalContextHash {
		return vaultrequests.ApprovalContext{}, accesscontrol.Capability{}, staleContext("Vault approval context hash is stale")
	}
	capability, err := r.validateAuthorization(ctx, request, approval)
	if err != nil {
		return vaultrequests.ApprovalContext{}, accesscontrol.Capability{}, err
	}
	return approval, capability, nil
}

type TransactionalObservation struct {
	Action  string
	Payload any
}

type TransactionalExecution struct {
	Run     func(context.Context, *sql.Tx) (any, []TransactionalObservation, error)
	Release func()
}

// PrepareTransactional validates and leases database-contained effects before
// the caller opens SQLCipher's single-connection transaction. External session
// effects deliberately stay on the compensating workflow.
func (r *Runtime) PrepareTransactional(ctx context.Context, request vaultrequests.Request) (TransactionalExecution, bool, error) {
	if request.ActionName != vaultrequests.ActionGenerateItem {
		return TransactionalExecution{}, false, nil
	}
	if err := r.validate(); err != nil {
		return TransactionalExecution{}, true, err
	}
	if _, _, err := r.authorizeRequest(ctx, request); err != nil {
		return TransactionalExecution{}, true, err
	}
	createInput, release, err := r.prepareGenerate(ctx, request)
	if err != nil {
		return TransactionalExecution{}, true, err
	}
	return TransactionalExecution{
		Release: release,
		Run: func(runCtx context.Context, tx *sql.Tx) (any, []TransactionalObservation, error) {
			output, payload, err := r.executeGenerateInTransaction(runCtx, tx, request, createInput)
			if err != nil {
				return nil, nil, err
			}
			return output, []TransactionalObservation{{Action: "vault.item.created", Payload: payload}}, nil
		},
	}, true, nil
}

func (r *Runtime) executeGenerateInTransaction(ctx context.Context, tx *sql.Tx, request vaultrequests.Request, createInput projectvault.CreateInput) (any, map[string]any, error) {
	if tx == nil {
		return nil, nil, ErrRuntimeUnavailable
	}
	item, err := r.itemMutations.Create(ctx, tx, createInput)
	if err != nil {
		return nil, nil, err
	}
	output := generatedItemOutput(item)
	payload := map[string]any{
		"vault_item_id": item.ID, "owner_project_id": item.OwnerProjectID,
		"secret_type": item.SecretType, "source": item.Source,
	}
	return output, payload, nil
}

func (r *Runtime) prepareGenerate(ctx context.Context, request vaultrequests.Request) (projectvault.CreateInput, func(), error) {
	if !r.allowGenerate(request.TokenID) {
		return projectvault.CreateInput{}, nil, errors.New("Vault generation rate limit exceeded; wait before generating another item")
	}
	input, err := vaultrequests.DecodeGenerateInput(request.Input)
	if err != nil {
		return projectvault.CreateInput{}, nil, err
	}
	if input.Name == "" || input.GeneratorKind == "" {
		return projectvault.CreateInput{}, nil, errors.New("name and generator_kind are required")
	}
	release, err := r.delivery.AcquireExclusive(ctx)
	if err != nil {
		return projectvault.CreateInput{}, nil, err
	}
	approval, err := vaultrequests.DecodeApprovalContext(request.ApprovalContext)
	if err != nil {
		release()
		return projectvault.CreateInput{}, nil, err
	}
	if _, err := r.validateAuthorization(ctx, request, approval); err != nil {
		release()
		return projectvault.CreateInput{}, nil, err
	}
	return generateCreateInput(request.ProjectID, input), release, nil
}

func generateCreateInput(projectID int64, input vaultrequests.GenerateInput) projectvault.CreateInput {
	return projectvault.CreateInput{
		Name: input.Name, OwnerProjectID: projectID, SharedProjectIDs: input.SharedProjectIDs,
		SecretType: input.SecretType, Provider: input.Provider, Environment: input.Environment,
		Description: input.Description, ExpiresAt: input.ExpiresAt,
		ExpiryWarningDays: input.ExpiryWarningDays, Source: "generated",
		GeneratorKind: input.GeneratorKind, Tags: input.Tags, UsageNotes: input.ProjectUsageNotes(),
	}
}

func generatedItemOutput(item projectvault.Item) map[string]any {
	return map[string]any{
		"item": map[string]any{
			"vault_ref": "vault:" + strconv.FormatInt(item.ID, 10),
			"item_id":   item.ID, "project_id": item.OwnerProjectID,
			"name": item.Name, "secret_type": item.SecretType, "status": item.Status,
			"expires_at": item.ExpiresAt, "value_version": item.ValueVersion,
			"metadata_revision": item.MetadataRevision,
		},
		"secret_returned": false,
	}
}

func (r *Runtime) executeSessionApply(
	ctx context.Context,
	request vaultrequests.Request,
	approval vaultrequests.ApprovalContext,
	capability accesscontrol.Capability,
) (any, error) {
	input, err := vaultrequests.DecodeSessionApplyInput(request.Input)
	if err != nil {
		return nil, err
	}
	snapshot := snapshotFromApproval(approval)
	if err := validateSnapshot(snapshot); err != nil {
		return nil, staleContext(err.Error())
	}
	local, err := r.LocalPrincipal()
	if err != nil {
		return nil, err
	}
	principal, err := r.TokenPrincipal(request.TokenID)
	if err != nil {
		return nil, err
	}
	authorize := func(authorizeCtx context.Context) error {
		_, authorizeErr := r.validateAuthorization(authorizeCtx, request, approval)
		return authorizeErr
	}
	releaseDelivery, err := r.delivery.AcquireDelivery(ctx)
	if err != nil {
		return nil, err
	}
	admission := newDeliveryAdmission(releaseDelivery)
	defer admission.releaseIfUnclaimed()
	ctx = withDeliveryAdmission(ctx, r.delivery)
	if err := authorize(ctx); err != nil {
		return nil, err
	}
	expiresAt, err := r.sessionLeaseExpiry(ctx, request, approval, capability)
	if err != nil {
		return nil, err
	}
	finalize := func(finalizeCtx context.Context, handle EnvironmentSessionHandle) error {
		if err := r.sessionItems.RecordSessionItems(finalizeCtx, handle.ID, approval.Items); err != nil {
			return err
		}
		lease := vaultsessions.Lease{
			WorkspaceID: r.workspaceID, RuntimeInstanceID: r.runtimeInstanceID,
			TokenID: request.TokenID, RuntimeID: handle.RuntimeID, SessionID: handle.ID,
			SessionGeneration: handle.Generation, ApprovalContextHash: request.ApprovalContextHash,
			EnvironmentContentHash: approval.EnvironmentContentHash, ExpiresAt: expiresAt,
			Validate: func(validateCtx context.Context) error {
				revoke := func(message string) error {
					_ = r.persistedLeases.Revoke(validateCtx, handle.ID, handle.Generation)
					return errors.New(message)
				}
				if _, err := r.validateAuthorization(validateCtx, request, approval); err != nil {
					return revoke("Vault authorization context changed: " + err.Error())
				}
				if err := r.sessionItems.RevalidateSession(validateCtx, approval.Items); err != nil {
					return revoke("Vault item context changed")
				}
				return nil
			},
		}
		if err := r.leases.Grant(lease); err != nil {
			return err
		}
		if err := r.persistedLeases.Grant(finalizeCtx, request.ProjectID, lease); err != nil {
			r.leases.RevokeSession(consoleSessionHandle(handle))
			return err
		}
		if err := r.sessionItems.MarkSessionItemsUsed(finalizeCtx, approval.Items); err != nil {
			r.leases.RevokeSession(consoleSessionHandle(handle))
			_ = r.persistedLeases.Revoke(finalizeCtx, handle.ID, handle.Generation)
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
	startupAdmissionRelease, err := admission.claim()
	if err != nil {
		return nil, err
	}
	createRequest := console.CreateRequest{
		RuntimeID: approval.RuntimeID, Name: fmt.Sprintf("Vault session for %s", request.ProjectName),
		CloseExisting: false, Cols: cols, Rows: rows, WaitForStart: true, Principal: principal,
		PrepareEnvironment:      consoleEnvironmentPreparer(r.environmentPreparer(snapshot, input.SessionSelections(), authorize, finalize, startupAdmissionRelease)),
		StartupAdmissionRelease: startupAdmissionRelease,
		EnvironmentContentHash:  approval.EnvironmentContentHash,
		ApprovalContextHash:     request.ApprovalContextHash,
	}
	expected := console.SessionHandle{}
	if approval.ExpectedSessionID > 0 {
		expected = console.SessionHandle{
			ID: approval.ExpectedSessionID, RuntimeID: approval.RuntimeID, Generation: approval.ExpectedGeneration,
		}
	}
	record, err := r.sessions.ReplaceIfCurrent(ctx, local, expected, createRequest)
	if errors.Is(err, console.ErrSessionChanged) || errors.Is(err, console.ErrSessionLimit) {
		return nil, staleContext("session identity changed; send a fresh request")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id": record.ID, "session_generation": record.Generation,
		"runtime_id": record.RuntimeID, "status": record.Status,
		"environment_names": itemNames(approval.Items), "expires_at": expiresAt.Format(time.RFC3339),
	}, nil
}

func withDeliveryAdmission(ctx context.Context, delivery DeliveryGate) context.Context {
	if delivery == nil {
		return ctx
	}
	return delivery.WithAdmission(ctx)
}

func consoleEnvironmentPreparer(preparer EnvironmentPreparer) console.EnvironmentPreparer {
	if preparer == nil {
		return nil
	}
	return func(ctx context.Context, peerIdentity string) (console.EnvironmentPreparation, error) {
		prepared, err := preparer(ctx, peerIdentity)
		if err != nil {
			return console.EnvironmentPreparation{}, err
		}
		var finalize func(context.Context, console.SessionHandle) error
		if prepared.Finalize != nil {
			finalize = func(finalizeCtx context.Context, handle console.SessionHandle) error {
				return prepared.Finalize(finalizeCtx, EnvironmentSessionHandle{
					ID: handle.ID, RuntimeID: handle.RuntimeID, Generation: handle.Generation,
				})
			}
		}
		return console.EnvironmentPreparation{
			Environment: prepared.Environment, Release: prepared.Release,
			PostValidate: prepared.PostValidate, Finalize: finalize,
		}, nil
	}
}

func consoleSessionHandle(handle EnvironmentSessionHandle) console.SessionHandle {
	return console.SessionHandle{ID: handle.ID, RuntimeID: handle.RuntimeID, Generation: handle.Generation}
}

func (r *Runtime) sessionLeaseExpiry(
	ctx context.Context,
	request vaultrequests.Request,
	approval vaultrequests.ApprovalContext,
	capability accesscontrol.Capability,
) (time.Time, error) {
	expiresAt := time.Now().UTC().Add(vaultsessions.MaxLeaseTTL)
	for _, raw := range []string{capability.ExpiresAt, approval.ConnectorPermissionExpiresAt} {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil && parsed.Before(expiresAt) {
			expiresAt = parsed
		}
	}
	token, err := r.tokens.Get(ctx, request.TokenID)
	if err != nil {
		return time.Time{}, fmt.Errorf("read Vault action token: %w", err)
	}
	if parsed, err := time.Parse(time.RFC3339, token.ExpiresAt); err == nil && parsed.Before(expiresAt) {
		expiresAt = parsed
	}
	return expiresAt, nil
}

func (r *Runtime) Compensate(ctx context.Context, request vaultrequests.Request, output any) error {
	if err := r.validate(); err != nil {
		return err
	}
	payload, _ := output.(map[string]any)
	switch request.ActionName {
	case vaultrequests.ActionRestartSession:
		return r.compensateSession(ctx, payload)
	case vaultrequests.ActionGenerateItem:
		return r.compensateGeneratedItem(ctx, payload)
	default:
		return nil
	}
}

func (r *Runtime) compensateSession(ctx context.Context, payload map[string]any) error {
	sessionID := jsonInt(payload["session_id"])
	if sessionID < 1 {
		return nil
	}
	runtimeID := jsonInt(payload["runtime_id"])
	generation := jsonInt(payload["session_generation"])
	if runtimeID > 0 && generation > 0 {
		r.leases.RevokeSession(console.SessionHandle{ID: sessionID, RuntimeID: runtimeID, Generation: generation})
	}
	var cleanupErrors []error
	if generation > 0 {
		if err := r.persistedLeases.Revoke(ctx, sessionID, generation); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	principal, err := r.LocalPrincipal()
	if err != nil {
		return err
	}
	if err := r.sessions.Close(ctx, principal, sessionID); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	return errors.Join(cleanupErrors...)
}

func (r *Runtime) compensateGeneratedItem(ctx context.Context, payload map[string]any) error {
	itemPayload, _ := payload["item"].(map[string]any)
	itemID := jsonInt(itemPayload["item_id"])
	valueVersion := jsonInt(itemPayload["value_version"])
	metadataRevision := jsonInt(itemPayload["metadata_revision"])
	if itemID < 1 || valueVersion < 1 || metadataRevision < 1 {
		return nil
	}
	release, err := r.delivery.AcquireExclusive(ctx)
	if err != nil {
		return err
	}
	defer release()
	return r.itemMutations.Delete(ctx, itemID, valueVersion, metadataRevision)
}

func itemNames(items []projectvault.SessionItem) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}
