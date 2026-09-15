package actions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const (
	actionFinishTimeout    = 10 * time.Second
	actionFinishAttempts   = 3
	actionFinishRetryDelay = 50 * time.Millisecond
	actionOrphanAge        = 30 * time.Second
)

func LeaseExpiry(now time.Time) time.Time { return now.Add(actionOrphanAge) }

func (r *Runtime) InsertTokenRequest(
	ctx context.Context,
	tokenID int64,
	prepared PreparedRequest,
	permission connectortargets.ActionPermission,
	status connectors.ResultStatus,
	errorText string,
	idempotencyKey string,
) (connectortargets.ActionRequest, bool, error) {
	return r.insertTokenRequest(ctx, tokenID, prepared, permission, status, errorText, idempotencyKey, false)
}

func (r *Runtime) InsertTokenRequestWithCapacity(
	ctx context.Context,
	tokenID int64,
	prepared PreparedRequest,
	permission connectortargets.ActionPermission,
	status connectors.ResultStatus,
	errorText string,
	idempotencyKey string,
) (connectortargets.ActionRequest, bool, error) {
	return r.insertTokenRequest(ctx, tokenID, prepared, permission, status, errorText, idempotencyKey, true)
}

func (r *Runtime) insertTokenRequest(
	ctx context.Context,
	tokenID int64,
	prepared PreparedRequest,
	permission connectortargets.ActionPermission,
	status connectors.ResultStatus,
	errorText string,
	idempotencyKey string,
	enforceCapacity bool,
) (connectortargets.ActionRequest, bool, error) {
	token, err := r.tokens.Get(ctx, tokenID, r.now().UTC())
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	tokenSnapshot, permissionSnapshot := approvalSnapshots(token, permission)
	approvalContext, approvalHash, err := BuildApprovalContext(prepared, tokenSnapshot, permissionSnapshot, r.now().UTC().Format(time.RFC3339))
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	return r.insertPreparedRequest(ctx, &tokenID, prepared, status, errorText, approvalContext, approvalHash, idempotencyKey, enforceCapacity)
}

func (r *Runtime) InsertPreparedRequest(
	ctx context.Context,
	tokenID *int64,
	prepared PreparedRequest,
	status connectors.ResultStatus,
	errorText string,
	approvalContext string,
	approvalHash string,
	idempotencyKey string,
) (connectortargets.ActionRequest, bool, error) {
	return r.insertPreparedRequest(ctx, tokenID, prepared, status, errorText, approvalContext, approvalHash, idempotencyKey, false)
}

func (r *Runtime) insertPreparedRequest(
	ctx context.Context,
	tokenID *int64,
	prepared PreparedRequest,
	status connectors.ResultStatus,
	errorText string,
	approvalContext string,
	approvalHash string,
	idempotencyKey string,
	enforceCapacity bool,
) (connectortargets.ActionRequest, bool, error) {
	redactedPreview, err := r.redactor.Preview(ctx, prepared.Action.Preview, prepared.ActionDefinition.SensitiveInputFields, prepared.ActionDefinition.OutputHint)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	redactedInput, err := r.redactor.Input(ctx, prepared.Requested.Input, prepared.ActionDefinition.SensitiveInputFields)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	sensitiveValues := actionresult.SensitiveValues(prepared.Requested.Input, prepared.Action.Payload, prepared.ActionDefinition.SensitiveInputFields)
	redactText := func(value string) (string, error) {
		redacted, redactErr := r.redactor.Text(ctx, value, actionresult.CredentialBoundary{})
		return actionresult.RedactSensitiveText(redacted, sensitiveValues), redactErr
	}
	redactedTitle, err := redactText(prepared.Action.Title)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	redactedSummary, err := redactText(prepared.Action.Summary)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	redactedReason, err := redactText(prepared.Requested.Reason)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	envelope := ExecutionEnvelope{
		Input: prepared.Requested.Input, Payload: prepared.Action.Payload,
		ApprovalPreview:      prepared.Action.Preview,
		SensitiveInputFields: append([]string(nil), prepared.ActionDefinition.SensitiveInputFields...),
		Reason:               prepared.Requested.Reason,
	}
	identityHash, err := r.IdempotencyIdentityHash(tokenID, prepared, idempotencyKey)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	insert := connectortargets.InsertActionRequestInput{
		TokenID: tokenID, TargetID: prepared.Target.ID, ProfileID: prepared.Profile.ID,
		ConnectorKind: prepared.Target.ConnectorKind, ActionName: prepared.Action.ActionName,
		Title: redactedTitle, Summary: redactedSummary,
		Preview: redactedPreview, Source: prepared.Requested.Source, Input: redactedInput,
		Reason: redactedReason, Status: status,
		ApprovalContext: approvalContext, ApprovalContextHash: approvalHash,
		RetryPolicy:    connectors.EffectiveRetryPolicy(prepared.ActionDefinition),
		IdempotencyKey: strings.TrimSpace(idempotencyKey), IdempotencyIdentityHash: identityHash,
		EnforceTokenCapacity: enforceCapacity,
	}
	if status == connectors.ResultRunning {
		_, runtimeInstanceID, identityErr := r.runtimeIdentity()
		if identityErr != nil {
			return connectortargets.ActionRequest{}, false, identityErr
		}
		insert.ExecutionOwner = runtimeInstanceID
		insert.ExecutionLeaseExpiresAt = LeaseExpiry(r.now().UTC()).Format(time.RFC3339Nano)
	}
	var request connectortargets.ActionRequest
	created := false
	err = r.mutations.WithMutation(ctx, "gateway", tokenID, 0, "connector_action.request.created",
		func() any { return RequestAuditPayload(request) },
		func(tx *sql.Tx) error {
			var insertErr error
			request, created, insertErr = connectortargets.NewTxStore(tx).InsertSealedActionRequestIdempotent(ctx, insert, func(requestID int64) (string, error) {
				sealed, sealErr := r.sealedRecords.SealActionRequest(requestID, envelope)
				if sealErr != nil {
					return "", fmt.Errorf("encrypt connector action payload: %w", sealErr)
				}
				return sealed, nil
			})
			if insertErr == nil && !created {
				return ErrMutationUnchanged
			}
			return insertErr
		},
	)
	if errors.Is(err, ErrMutationUnchanged) {
		return request, false, nil
	}
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	if errorText == "" {
		return request, created, nil
	}
	allowed := []connectors.ResultStatus(nil)
	if status == connectors.ResultBlocked {
		allowed = []connectors.ResultStatus{connectors.ResultBlocked}
	}
	finished, finishErr := r.FinishAllowed(ctx, request.ID, status, nil, "", errorText, allowed, prepared.ActionDefinition.OutputHint)
	return finished, true, finishErr
}

func (r *Runtime) IdempotencyIdentityHash(tokenID *int64, prepared PreparedRequest, key string) (string, error) {
	input := prepared.IdempotencyInput
	if input == nil {
		input = prepared.Requested.Input
	}
	return r.CallIdentityHash(tokenID, prepared.Requested.Source,
		connectors.FormatTargetRef(prepared.Target.ConnectorKind, prepared.Target.ID, prepared.Profile.ID),
		prepared.Action.ActionName, input, prepared.Requested.Reason, key)
}

func (r *Runtime) CallIdentityHash(tokenID *int64, source, targetRef, actionName string, input map[string]any, reason, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", nil
	}
	if input == nil {
		input = map[string]any{}
	}
	connectorKind, targetID, profileID, ok := connectors.ParseTargetRef(targetRef)
	if !ok {
		return "", connectortargets.ErrInvalidTargetRef
	}
	identity := struct {
		TokenID       *int64         `json:"token_id,omitempty"`
		Source        string         `json:"source"`
		TargetID      int64          `json:"target_id"`
		ProfileID     int64          `json:"profile_id"`
		ConnectorKind string         `json:"connector_kind"`
		ActionName    string         `json:"action_name"`
		Input         map[string]any `json:"input"`
		Reason        string         `json:"reason"`
	}{
		TokenID: tokenID, Source: source, TargetID: targetID, ProfileID: profileID,
		ConnectorKind: connectorKind, ActionName: actionName, Input: input, Reason: reason,
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("encode connector action idempotency identity: %w", err)
	}
	return r.identityTag(encoded)
}

func (r *Runtime) Replay(ctx context.Context, tokenID *int64, call Call) (CallResult, bool, error) {
	key := strings.TrimSpace(call.IdempotencyKey)
	if key == "" {
		return CallResult{}, false, nil
	}
	request, err := connectortargets.NewStore(r.database).GetActionRequestByIdempotency(ctx, tokenID, call.Source, key)
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		return CallResult{}, false, nil
	}
	if err != nil {
		return CallResult{}, false, err
	}
	identityHash, err := r.CallIdentityHash(tokenID, call.Source, call.TargetRef, call.ActionName, call.Input, call.Reason, key)
	if err != nil {
		return CallResult{}, false, err
	}
	if request.IdempotencyIdentityHash != identityHash {
		return CallResult{}, false, connectortargets.ErrActionRequestIdempotency
	}
	return replayedCallResult(request), true, nil
}

func replayedCallResult(request connectortargets.ActionRequest) CallResult {
	errorText := request.Error
	if request.Status == connectors.ResultApprovalPending && errorText == "" {
		errorText = "Waiting for user approval."
	}
	return CallResult{Request: request, Result: connectors.ActionResult{
		Status: request.Status, Output: request.Output, DisplayText: request.DisplayText, Error: errorText,
		Handles: connectors.ActionHandles{RequestID: request.ID, FollowupTool: "get_connector_action_request"},
	}, Replayed: true}
}

func FailureOutput(err error) any {
	code := connectors.ErrorCode(err)
	if code == "" {
		return nil
	}
	details := connectors.ErrorDetails(err)
	if details == nil {
		details = map[string]any{}
	}
	details["code"] = code
	return details
}

func RequestAuditPayload(request connectortargets.ActionRequest) map[string]any {
	return map[string]any{
		"request_id": request.ID, "target_id": request.TargetID, "profile_id": request.ProfileID,
		"connector_kind": request.ConnectorKind, "action_name": request.ActionName, "status": request.Status,
	}
}
