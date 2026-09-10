package actions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

const approvalNoteMaxBytes = 8 << 10

type pendingExecution struct {
	request   connectortargets.ActionRequest
	prepared  PreparedRequest
	snapshot  ExecutionSnapshot
	principal executionprincipal.Principal
	targetRef string
	userNote  string
}

func ValidateApprovalNote(value string) error {
	if len([]byte(value)) <= approvalNoteMaxBytes {
		return nil
	}
	return fmt.Errorf("user_note must be %d bytes or less", approvalNoteMaxBytes)
}

func (r *Runtime) RunPending(ctx context.Context, id int64, userNote string) (connectortargets.ActionRequest, error) {
	if err := r.validate(); err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if !r.mcpStarted() {
		return connectortargets.ActionRequest{}, ErrMCPExecutionStopped
	}
	userNote = strings.TrimSpace(userNote)
	if err := ValidateApprovalNote(userNote); err != nil {
		return connectortargets.ActionRequest{}, err
	}
	release, err := r.delivery.Acquire(ctx)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	claimHeld := true
	defer func() {
		if claimHeld {
			release()
		}
	}()
	execution, err := r.preparePending(ctx, id, userNote)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	r.TrackCredentialBoundary(execution.request.ID, execution.snapshot.CredentialBoundary)
	if _, err := r.markPendingRunning(ctx, execution.request, execution.userNote); err != nil {
		r.ClearCredentialBoundary(execution.request.ID)
		return connectortargets.ActionRequest{}, err
	}
	release()
	claimHeld = false
	return r.executePending(ctx, execution)
}

func (r *Runtime) DeclinePending(ctx context.Context, id int64, userNote string) (connectortargets.ActionRequest, error) {
	if err := r.validate(); err != nil {
		return connectortargets.ActionRequest{}, err
	}
	userNote = strings.TrimSpace(userNote)
	if err := ValidateApprovalNote(userNote); err != nil {
		return connectortargets.ActionRequest{}, err
	}
	release, err := r.delivery.Acquire(ctx)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	defer release()
	message := "User declined the connector action"
	if userNote != "" {
		message += ": " + userNote
	}
	redacted, err := r.redactOperatorText(ctx, id, message)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	var item connectortargets.ActionRequest
	err = r.mutations.WithMutation(ctx, "user", nil, 0, "connector_action.request.declined",
		func() any { return RequestAuditPayload(item) },
		func(tx *sql.Tx) error {
			var declineErr error
			item, declineErr = connectortargets.NewTxStore(tx).DeclineActionRequest(ctx, id, redacted)
			return declineErr
		},
	)
	return item, err
}

func (r *Runtime) ApprovalPreview(ctx context.Context, item connectortargets.ActionRequest) (map[string]any, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if item.Status != connectors.ResultApprovalPending || strings.TrimSpace(item.EncryptedPayloadJSON) == "" {
		return item.Preview, nil
	}
	release, err := r.delivery.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	envelope, err := r.sealedRecords.OpenActionRequest(item.ID, item.EncryptedPayloadJSON)
	if err != nil {
		return nil, fmt.Errorf("decrypt connector approval preview: %w", err)
	}
	if envelope.ApprovalPreview == nil {
		return item.Preview, nil
	}
	boundary, err := r.CredentialBoundaryForRequest(ctx, item.ID)
	if err != nil {
		return nil, fmt.Errorf("load connector approval credential boundary: %w", err)
	}
	redacted, ok := boundary.RedactStructured(envelope.ApprovalPreview).(map[string]any)
	if !ok {
		return nil, errors.New("redact connector approval preview")
	}
	return redacted, nil
}

func (r *Runtime) preparePending(ctx context.Context, id int64, userNote string) (pendingExecution, error) {
	store := connectortargets.NewStore(r.database)
	item, err := store.GetActionRequest(ctx, id)
	if err != nil {
		return pendingExecution{}, err
	}
	if item.Status != connectors.ResultApprovalPending {
		return pendingExecution{}, connectortargets.ErrActionRequestNotPending
	}
	if strings.TrimSpace(item.EncryptedPayloadJSON) == "" ||
		strings.TrimSpace(item.ApprovalContext) == "" ||
		strings.TrimSpace(item.ApprovalContextHash) == "" {
		reason := "connector approval integrity data is missing; ask the AI to send a fresh request"
		return pendingExecution{}, r.staleApproval(ctx, item.ID, reason, reason, "request_integrity")
	}
	token, err := r.currentApprovalToken(ctx, item)
	if err != nil {
		return pendingExecution{}, err
	}
	rawInput, rawPayload, rawReason, err := r.executionPayload(item)
	if err != nil {
		reason := "connector approval integrity data is invalid; ask the AI to send a fresh request"
		return pendingExecution{}, r.staleApproval(ctx, item.ID, reason, reason, "request_integrity")
	}
	targetRef := connectors.FormatTargetRef(item.ConnectorKind, item.TargetID, item.ProfileID)
	prepared, err := r.service.Prepare(ctx, PrepareRequest{
		Source: SourceMCP, TargetRef: targetRef, ActionName: item.ActionName,
		Input: rawInput, Reason: rawReason, CreatedAt: r.now().UTC(),
	})
	if err != nil {
		reason := "connector approval context changed; ask the AI to send a fresh request"
		return pendingExecution{}, r.staleApproval(ctx, item.ID, reason, reason, "target_or_action")
	}
	permission, err := store.GetActionPermission(ctx, token.ID, item.TargetID, item.ProfileID, item.ActionName, r.now().UTC())
	if err != nil && !errors.Is(err, connectortargets.ErrActionPermissionNotFound) {
		return pendingExecution{}, err
	}
	if errors.Is(err, connectortargets.ErrActionPermissionNotFound) || permission.ExecutionRule != connectortargets.ActionPermissionApprovalRequired {
		reason := "connector approval context changed; ask the AI to send a fresh request"
		if _, staleErr := r.FinishStaleApproval(ctx, item.ID, reason, "permission"); staleErr != nil {
			return pendingExecution{}, staleErr
		}
		return pendingExecution{}, errors.New(reason)
	}
	tokenSnapshot, permissionSnapshot := approvalSnapshots(token, permission)
	currentContext, currentHash, err := BuildApprovalContext(prepared, tokenSnapshot, permissionSnapshot, r.now().UTC().Format(time.RFC3339))
	if err != nil {
		return pendingExecution{}, err
	}
	if item.ApprovalContextHash != currentHash {
		drift := ApprovalDriftReason(item.ApprovalContext, currentContext)
		reason := "connector approval context changed; ask the AI to send a fresh request"
		return pendingExecution{}, r.staleApproval(ctx, item.ID, reason, reason, drift)
	}
	prepared.Action.Payload = rawPayload
	if userNote != "" {
		userNote, err = r.redactOperatorText(ctx, item.ID, userNote)
		if err != nil {
			return pendingExecution{}, err
		}
		userNote = strings.TrimSpace(userNote)
		message := "Operator approved the connector action with note: " + userNote
		if len([]byte(message)) > approvalNoteMaxBytes {
			return pendingExecution{}, fmt.Errorf("message must be %d bytes or less", approvalNoteMaxBytes)
		}
	}
	principal, err := r.tokenPrincipal(token.ID)
	if err != nil {
		return pendingExecution{}, err
	}
	snapshot, err := r.Snapshot(ctx, prepared)
	if err != nil {
		reason := "connector approval context changed; ask the AI to send a fresh request"
		return pendingExecution{}, r.staleApproval(ctx, item.ID, reason, reason, "profile")
	}
	return pendingExecution{
		request: item, prepared: prepared, snapshot: snapshot, principal: principal,
		targetRef: targetRef, userNote: userNote,
	}, nil
}

func (r *Runtime) currentApprovalToken(ctx context.Context, item connectortargets.ActionRequest) (AuthorizationToken, error) {
	if item.TokenID == nil {
		storedReason := "connector approval token no longer exists"
		responseReason := storedReason + "; ask the AI to send a fresh request"
		return AuthorizationToken{}, r.staleApproval(ctx, item.ID, storedReason, responseReason, "token")
	}
	token, err := r.tokens.Get(ctx, *item.TokenID, r.now().UTC())
	if err != nil && !errors.Is(err, ErrTokenNotFound) {
		return AuthorizationToken{}, err
	}
	if errors.Is(err, ErrTokenNotFound) {
		reason := "connector approval token no longer exists; ask the AI to send a fresh request"
		return AuthorizationToken{}, r.staleApproval(ctx, item.ID, reason, reason, "token")
	}
	if token.Active {
		return token, nil
	}
	reason := "connector approval token is no longer valid; ask the AI to send a fresh request"
	if token.RevokedAt != "" {
		reason = "connector approval token was revoked; ask the AI to send a fresh request"
	} else {
		reason = "connector approval token expired; ask the AI to send a fresh request"
	}
	return AuthorizationToken{}, r.staleApproval(ctx, item.ID, reason, reason, "token")
}

func (r *Runtime) staleApproval(ctx context.Context, requestID int64, storedReason string, responseReason string, drift string) error {
	if _, err := r.FinishStaleApproval(ctx, requestID, storedReason, drift); err != nil {
		return err
	}
	return errors.New(responseReason)
}

func (r *Runtime) markPendingRunning(ctx context.Context, item connectortargets.ActionRequest, userNote string) (connectortargets.ActionRequest, error) {
	_, runtimeInstanceID, err := r.runtimeIdentity()
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	var running connectortargets.ActionRequest
	err = r.mutations.WithMutation(ctx, "user", item.TokenID, 0, "connector_action.request.running",
		func() any { return RequestAuditPayload(running) },
		func(tx *sql.Tx) error {
			var markErr error
			running, markErr = connectortargets.NewTxStore(tx).MarkActionRequestRunning(ctx, item.ID, runtimeInstanceID, LeaseExpiry(r.now().UTC()))
			if markErr != nil || userNote == "" {
				return markErr
			}
			if item.TokenID == nil {
				return errors.New("connector approval token is missing")
			}
			return r.enqueueUserNote(ctx, tx, *item.TokenID, "Operator approved the connector action with note: "+userNote)
		},
	)
	return running, err
}

func (r *Runtime) executePending(ctx context.Context, execution pendingExecution) (connectortargets.ActionRequest, error) {
	item, prepared := execution.request, execution.prepared
	clearBoundary := true
	defer func() {
		if clearBoundary {
			r.ClearCredentialBoundary(item.ID)
		}
	}()
	release, err := r.delivery.Acquire(ctx)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	claimHeld := true
	defer func() {
		if claimHeld {
			release()
		}
	}()
	prepared, err = r.Revalidate(ctx, item, prepared, connectortargets.ActionPermissionApprovalRequired)
	if errors.Is(err, ErrConnectorAuthorizationChanged) {
		return r.FinishStaleApproval(ctx, item.ID, err.Error(), "authorization")
	}
	if err != nil {
		return r.Finish(ctx, item.ID, connectors.ResultFailed, nil, "", err.Error(), prepared.ActionDefinition.OutputHint)
	}
	snapshot, err := r.Snapshot(ctx, prepared)
	if err != nil {
		return r.FinishStaleApproval(ctx, item.ID, err.Error(), "profile")
	}
	execution.snapshot = snapshot
	r.TrackCredentialBoundary(item.ID, snapshot.CredentialBoundary)
	claimed, dispatched, err := r.BeginDispatch(ctx, item.ID)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if !dispatched {
		return claimed, nil
	}
	release()
	claimHeld = false
	result, err := r.ExecutePrepared(ctx, execution.principal, prepared, snapshot)
	if err != nil {
		finished, finishErr := r.Finish(context.Background(), item.ID, ExecutionFailureStatus(err), FailureOutput(err), "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectortargets.ActionRequest{}, NewTerminalPersistenceError(item.ID, finishErr)
		}
		return finished, nil
	}
	status := result.Status
	item, err = r.CaptureSessionHandleIfReturned(ctx, item, result.Handles)
	if err != nil {
		finished, finishErr := r.Finish(context.Background(), item.ID, connectors.ResultOutcomeUnknown, nil, "", HandlePersistenceError, prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectortargets.ActionRequest{}, NewTerminalPersistenceError(item.ID, errors.Join(err, finishErr))
		}
		return finished, nil
	}
	if status == connectors.ResultRunning {
		if !r.runningActions.SupportsRunning(prepared) {
			finished, finishErr := r.Finish(context.Background(), item.ID, connectors.ResultError, nil, "", "connector returned running for an action that does not support asynchronous execution", prepared.ActionDefinition.OutputHint)
			if finishErr != nil {
				return connectortargets.ActionRequest{}, NewTerminalPersistenceError(item.ID, finishErr)
			}
			r.observeApproval(ctx, item, execution, "error", false)
			return finished, nil
		}
		result.Handles.RequestID = item.ID
		if result.Handles.FollowupTool == "" {
			result.Handles.FollowupTool = "get_connector_action_request"
		}
		go func() {
			defer r.ClearCredentialBoundary(item.ID)
			r.runningActions.FinishRunning(item.ID, prepared, execution.principal, result.Handles)
		}()
		clearBoundary = false
		running, getErr := connectortargets.NewStore(r.database).GetActionRequest(context.Background(), item.ID)
		if getErr != nil {
			return connectortargets.ActionRequest{}, NewTerminalPersistenceError(item.ID, getErr)
		}
		r.observeApproval(ctx, item, execution, "running", false)
		return running, nil
	}
	if status == connectors.ResultApprovalPending {
		status = connectors.ResultFailed
		result.Error = "connector returned approval_pending after approval was already granted"
	}
	finished, err := r.Finish(context.Background(), item.ID, status, result.Output, result.DisplayText, result.Error, prepared.ActionDefinition.OutputHint)
	if err != nil {
		return connectortargets.ActionRequest{}, NewTerminalPersistenceError(item.ID, err)
	}
	r.observeApproval(ctx, item, execution, string(finished.Status), true)
	return finished, nil
}

func (r *Runtime) observeApproval(ctx context.Context, item connectortargets.ActionRequest, execution pendingExecution, suffix string, includeNote bool) {
	payload := map[string]any{
		"request_id": item.ID, "target_ref": execution.targetRef,
		"connector_kind": item.ConnectorKind, "action_name": item.ActionName,
	}
	if includeNote {
		payload["note"] = execution.userNote != ""
	}
	r.mutations.Observe(ctx, "user", item.TokenID, 0, "connector_action.run."+suffix, payload)
}

func (r *Runtime) FinishStaleApproval(ctx context.Context, requestID int64, reason string, drift string) (connectortargets.ActionRequest, error) {
	var stale connectortargets.ActionRequest
	err := r.mutations.WithMutation(ctx, "gateway", nil, 0, "connector_action.request.stale",
		func() any { return RequestAuditPayload(stale) },
		func(tx *sql.Tx) error {
			var finishErr error
			stale, finishErr = connectortargets.NewTxStore(tx).FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
				ID: requestID, Status: connectors.ResultStale, Error: reason,
				ApprovalDrift: drift, AllowedStatuses: ApprovalFinishStatuses(),
			})
			return finishErr
		},
	)
	return stale, err
}

func ApprovalFinishStatuses() []connectors.ResultStatus {
	return []connectors.ResultStatus{connectors.ResultApprovalPending, connectors.ResultRunning}
}

func (r *Runtime) executionPayload(item connectortargets.ActionRequest) (map[string]any, map[string]any, string, error) {
	if strings.TrimSpace(item.EncryptedPayloadJSON) == "" {
		return nil, nil, "", errors.New("connector action encrypted execution payload is missing")
	}
	envelope, err := r.sealedRecords.OpenActionRequest(item.ID, item.EncryptedPayloadJSON)
	if err != nil {
		return nil, nil, "", fmt.Errorf("decrypt connector action execution payload: %w", err)
	}
	if envelope.Input == nil || envelope.Payload == nil {
		return nil, nil, "", errors.New("connector action encrypted execution payload is incomplete")
	}
	return cloneMap(envelope.Input), cloneMap(envelope.Payload), envelope.Reason, nil
}

func (r *Runtime) redactOperatorText(ctx context.Context, requestID int64, value string) (string, error) {
	boundary, err := r.CredentialBoundaryForRequest(ctx, requestID)
	if err != nil {
		return "", fmt.Errorf("load connector credential boundary for operator text: %w", err)
	}
	return r.redactor.Text(ctx, value, boundary)
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
