package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

func (s *Server) insertConnectorActionRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	tokenID int64,
	prepared actions.PreparedRequest,
	permission connectortargets.ActionPermission,
	status connectors.ResultStatus,
	errorText string,
	idempotencyKey string,
) (connectortargets.ActionRequest, bool, error) {
	capturedAt := time.Now().UTC().Format(time.RFC3339)
	token, err := runtime.tokens.Get(ctx, tokenID)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	approvalContext, approvalHash, err := connectorApprovalContext(prepared, token, permission, capturedAt)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	return s.insertPreparedConnectorActionRequest(ctx, runtime, &tokenID, prepared, status, errorText, approvalContext, approvalHash, idempotencyKey)
}

func (s *Server) insertPreparedConnectorActionRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	tokenID *int64,
	prepared actions.PreparedRequest,
	status connectors.ResultStatus,
	errorText string,
	approvalContext string,
	approvalHash string,
	idempotencyKey string,
) (connectortargets.ActionRequest, bool, error) {
	redactedPreview, err := s.redactConnectorActionPreview(ctx, runtime, prepared.Action.Preview, prepared.ActionDefinition.SensitiveInputFields, prepared.ActionDefinition.OutputHint)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	redactedInput, err := s.redactConnectorActionInput(ctx, runtime, prepared.Requested.Input, prepared.ActionDefinition.SensitiveInputFields)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	redactText := func(value string) string {
		value = s.redactForPersistence(ctx, runtime, value)
		return redactConnectorActionSensitiveText(value, connectorActionSensitiveValues(
			prepared.Requested.Input,
			prepared.Action.Payload,
			prepared.ActionDefinition.SensitiveInputFields,
		))
	}
	executionEnvelope := connectorActionExecutionEnvelope{
		Input:                prepared.Requested.Input,
		Payload:              prepared.Action.Payload,
		ApprovalPreview:      prepared.Action.Preview,
		SensitiveInputFields: append([]string(nil), prepared.ActionDefinition.SensitiveInputFields...),
		Reason:               prepared.Requested.Reason,
	}
	identityHash, err := connectorActionIdempotencyIdentityHash(runtime, tokenID, prepared, idempotencyKey)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	insertInput := connectortargets.InsertActionRequestInput{
		TokenID:                 tokenID,
		TargetID:                prepared.Target.ID,
		ProfileID:               prepared.Profile.ID,
		ConnectorKind:           prepared.Target.ConnectorKind,
		ActionName:              prepared.Action.ActionName,
		Title:                   redactText(prepared.Action.Title),
		Summary:                 redactText(prepared.Action.Summary),
		Preview:                 redactedPreview,
		Source:                  prepared.Requested.Source,
		Input:                   redactedInput,
		EncryptedPayloadJSON:    "",
		Reason:                  redactText(prepared.Requested.Reason),
		Status:                  status,
		ApprovalContext:         approvalContext,
		ApprovalContextHash:     approvalHash,
		RetryPolicy:             connectors.EffectiveRetryPolicy(prepared.ActionDefinition),
		IdempotencyKey:          strings.TrimSpace(idempotencyKey),
		IdempotencyIdentityHash: identityHash,
	}
	if status == connectors.ResultRunning {
		if err := ensureRuntimeIdentity(runtime); err != nil {
			return connectortargets.ActionRequest{}, false, err
		}
		insertInput.ExecutionOwner = runtime.runtimeInstanceID
		insertInput.ExecutionLeaseExpiresAt = connectorActionLeaseExpiry(time.Now().UTC()).Format(time.RFC3339Nano)
	}
	var request connectortargets.ActionRequest
	created := false
	err = s.withAuditedMutation(
		ctx, runtime, "gateway", tokenID, 0, "connector_action.request.created",
		func() any { return connectorActionRequestAuditPayload(request) },
		func(tx *sql.Tx) error {
			var err error
			store := connectortargets.NewTxStore(tx)
			request, created, err = store.InsertSealedActionRequestIdempotent(ctx, insertInput, func(requestID int64) (string, error) {
				payload, encryptErr := recordcrypto.EncryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorActionRequest, requestID, executionEnvelope)
				if encryptErr != nil {
					return "", fmt.Errorf("encrypt connector action payload: %w", encryptErr)
				}
				return payload, nil
			})
			if err == nil && !created {
				return errAuditedMutationUnchanged
			}
			if err != nil {
				return err
			}
			return nil
		},
	)
	if errors.Is(err, errAuditedMutationUnchanged) {
		return request, false, nil
	}
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	if !created {
		return request, false, nil
	}
	if errorText != "" {
		if status == connectors.ResultBlocked {
			finished, finishErr := s.finishConnectorActionRequestWithAllowed(ctx, runtime, request.ID, status, nil, "", errorText, []connectors.ResultStatus{connectors.ResultBlocked}, prepared.ActionDefinition.OutputHint)
			return finished, true, finishErr
		}
		finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, status, nil, "", errorText, prepared.ActionDefinition.OutputHint)
		return finished, true, finishErr
	}
	return request, true, nil
}

func connectorActionIdempotencyIdentityHash(runtime *databaseRuntime, tokenID *int64, prepared actions.PreparedRequest, key string) (string, error) {
	input := prepared.IdempotencyInput
	if input == nil {
		input = prepared.Requested.Input
	}
	return connectorActionCallIdentityHash(
		runtime,
		tokenID,
		prepared.Requested.Source,
		connectortargets.ConnectorTargetRef(prepared.Target.ConnectorKind, prepared.Target.ID, prepared.Profile.ID),
		prepared.Action.ActionName,
		input,
		prepared.Requested.Reason,
		key,
	)
}

func connectorActionCallIdentityHash(runtime *databaseRuntime, tokenID *int64, source, targetRef, actionName string, input map[string]any, reason, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", nil
	}
	if input == nil {
		input = map[string]any{}
	}
	connectorKind, targetID, profileID, ok := connectortargets.ParseConnectorTargetRef(targetRef)
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
	if runtime == nil {
		return "", fmt.Errorf("connector action runtime is unavailable")
	}
	return connectorActionIdentityTag(runtime.actionIdentityKey, encoded)
}

func replayConnectorActionCall(ctx context.Context, runtime *databaseRuntime, tokenID *int64, call connectorActionCall) (connectorActionCallResult, bool, error) {
	key := strings.TrimSpace(call.IdempotencyKey)
	if key == "" {
		return connectorActionCallResult{}, false, nil
	}
	request, err := connectortargets.NewStore(runtime.database).GetActionRequestByIdempotency(ctx, tokenID, call.Source, key)
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		return connectorActionCallResult{}, false, nil
	}
	if err != nil {
		return connectorActionCallResult{}, false, err
	}
	identityHash, err := connectorActionCallIdentityHash(runtime, tokenID, call.Source, call.TargetRef, call.ActionName, call.Input, call.Reason, key)
	if err != nil {
		return connectorActionCallResult{}, false, err
	}
	if request.IdempotencyIdentityHash != identityHash {
		return connectorActionCallResult{}, false, connectortargets.ErrActionRequestIdempotency
	}
	return replayedConnectorActionCallResult(request), true, nil
}

func replayedConnectorActionCallResult(request connectortargets.ActionRequest) connectorActionCallResult {
	errorText := request.Error
	if request.Status == connectors.ResultApprovalPending && errorText == "" {
		errorText = "Waiting for user approval."
	}
	return connectorActionCallResult{Request: request, Result: connectors.ActionResult{
		Status: request.Status, Output: request.Output, DisplayText: request.DisplayText, Error: errorText,
		Handles: connectors.ActionHandles{RequestID: request.ID, FollowupTool: "get_connector_action_request"},
	}, Replayed: true}
}

func connectorActionFailureOutput(err error) any {
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

func connectorActionRequestAuditPayload(request connectortargets.ActionRequest) map[string]any {
	return map[string]any{
		"request_id": request.ID, "target_id": request.TargetID, "profile_id": request.ProfileID,
		"connector_kind": request.ConnectorKind, "action_name": request.ActionName,
		"status": request.Status,
	}
}
