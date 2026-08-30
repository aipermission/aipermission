package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

var errMCPExecutionStopped = errors.New("MCP execution is stopped")

const (
	connectorActionToolName          = "connector.call_action"
	connectorActionApprovalHint      = "Wait 3 seconds, then poll this connector action request until it is completed, failed, declined, stale, blocked, or outcome_unknown."
	connectorActionHandleError       = "connector action returned a session handle that could not be persisted; inspect the target state before retrying because the remote outcome is unknown"
	connectorActionRunningHint       = "Wait 3 seconds, then call get_connector_action_request again. Use the connector-specific read or recovery actions when the connector exposes them."
	connectorActionMissingPermission = "This token is not allowed to run this connector action for the selected target/profile"
	connectorActionFinishTimeout     = 10 * time.Second
)

type connectorActionCall struct {
	Source         string
	TokenID        int64
	TargetRef      string
	ActionName     string
	Input          map[string]any
	Reason         string
	IdempotencyKey string
}

type connectorActionCallResult struct {
	Request    connectortargets.ActionRequest
	Permission connectortargets.ActionPermission
	Result     connectors.ActionResult
	Replayed   bool
}

type connectorActionExecutionOptions struct {
	Permission              connectortargets.ActionPermission
	UnsupportedRunningError string
	ApprovalPendingError    string
	FollowupTool            string
}

type connectorActionExecutionEnvelope struct {
	Input           map[string]any `json:"input"`
	Payload         map[string]any `json:"payload"`
	ApprovalPreview map[string]any `json:"approval_preview,omitempty"`
	Reason          string         `json:"reason,omitempty"`
}

type connectorSecretAccessor struct {
	values map[string]any
}

type connectorActionExecutionSnapshot struct {
	secrets map[string]any
}

func (a connectorSecretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := a.values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	default:
		return fmt.Sprint(typed), nil
	}
}

type noopConnectorEventSink struct{}

func (noopConnectorEventSink) Emit(context.Context, connectors.ActionEvent) error { return nil }

func (s *Server) callConnectorAction(ctx context.Context, runtime *databaseRuntime, call connectorActionCall) (connectorActionCallResult, error) {
	if runtime == nil || runtime.database == nil {
		return connectorActionCallResult{}, fmt.Errorf("database runtime is not available")
	}
	if call.TokenID < 1 {
		return connectorActionCallResult{}, fmt.Errorf("token_id is required")
	}
	if call.Source == "" {
		call.Source = commandRequestSourceMCP
	}
	tokenID := call.TokenID
	if replay, ok, err := replayConnectorActionCall(ctx, runtime, &tokenID, call); err != nil {
		return connectorActionCallResult{}, err
	} else if ok {
		return replay, nil
	}
	if call.Source == commandRequestSourceMCP && !runtime.isMCPStarted() {
		return connectorActionCallResult{}, errMCPExecutionStopped
	}
	prepared, err := runtime.prepareConnectorAction(ctx, actions.PrepareRequest{
		Source:     call.Source,
		TargetRef:  call.TargetRef,
		ActionName: call.ActionName,
		Input:      call.Input,
		Reason:     call.Reason,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		return connectorActionCallResult{}, err
	}

	store := connectortargets.NewStore(runtime.database)
	permission, err := store.GetActionPermission(ctx, call.TokenID, prepared.Target.ID, prepared.Profile.ID, prepared.Action.ActionName, time.Now().UTC())
	if errors.Is(err, connectortargets.ErrActionPermissionNotFound) {
		request, created, insertErr := s.insertConnectorActionRequest(ctx, runtime, call.TokenID, prepared, connectortargets.ActionPermission{}, connectors.ResultBlocked, connectorActionMissingPermission, call.IdempotencyKey)
		if insertErr != nil {
			return connectorActionCallResult{}, insertErr
		}
		if !created {
			return replayedConnectorActionCallResult(request), nil
		}
		return connectorActionCallResult{Request: request, Result: connectors.ActionResult{Status: connectors.ResultBlocked, Error: connectorActionMissingPermission}}, nil
	}
	if err != nil {
		return connectorActionCallResult{}, err
	}
	if permission.ExecutionRule == connectortargets.ActionPermissionBlocked {
		request, created, insertErr := s.insertConnectorActionRequest(ctx, runtime, call.TokenID, prepared, permission, connectors.ResultBlocked, "Connector action is blocked for this token", call.IdempotencyKey)
		if insertErr != nil {
			return connectorActionCallResult{}, insertErr
		}
		if !created {
			return replayedConnectorActionCallResult(request), nil
		}
		return connectorActionCallResult{
			Request:    request,
			Permission: permission,
			Result:     connectors.ActionResult{Status: connectors.ResultBlocked, Error: "Connector action is blocked for this token"},
		}, nil
	}
	if permission.ExecutionRule == connectortargets.ActionPermissionApprovalRequired {
		request, created, insertErr := s.insertConnectorActionRequest(ctx, runtime, call.TokenID, prepared, permission, connectors.ResultApprovalPending, "", call.IdempotencyKey)
		if insertErr != nil {
			return connectorActionCallResult{}, insertErr
		}
		if !created {
			return replayedConnectorActionCallResult(request), nil
		}
		return connectorActionCallResult{
			Request:    request,
			Permission: permission,
			Result: connectors.ActionResult{
				Status: connectors.ResultApprovalPending,
				Error:  "Waiting for user approval.",
				Handles: connectors.ActionHandles{
					RequestID:    request.ID,
					FollowupTool: "get_connector_action_request",
				},
			},
		}, nil
	}

	principal, err := tokenExecutionPrincipal(runtime, call.TokenID)
	if err != nil {
		return connectorActionCallResult{}, err
	}
	request, created, err := s.insertConnectorActionRequest(ctx, runtime, call.TokenID, prepared, permission, connectors.ResultRunning, "", call.IdempotencyKey)
	if err != nil {
		return connectorActionCallResult{}, err
	}
	if !created {
		return replayedConnectorActionCallResult(request), nil
	}
	return s.executeInsertedConnectorAction(ctx, runtime, prepared, request, principal, connectorActionExecutionOptions{
		Permission:              permission,
		UnsupportedRunningError: "connector returned running for an action that does not support asynchronous execution",
		ApprovalPendingError:    "connector returned approval_pending after execution was already allowed",
		FollowupTool:            "get_connector_action_request",
	})
}

func (s *Server) runLocalConnectorAction(ctx context.Context, runtime *databaseRuntime, call connectorActionCall) (connectorActionCallResult, error) {
	if runtime == nil || runtime.database == nil {
		return connectorActionCallResult{}, fmt.Errorf("database runtime is not available")
	}
	if call.Source == "" {
		call.Source = commandRequestSourceManual
	}
	if replay, ok, err := replayConnectorActionCall(ctx, runtime, nil, call); err != nil {
		return connectorActionCallResult{}, err
	} else if ok {
		return replay, nil
	}
	prepared, err := runtime.prepareConnectorAction(ctx, actions.PrepareRequest{
		Source:     call.Source,
		TargetRef:  call.TargetRef,
		ActionName: call.ActionName,
		Input:      call.Input,
		Reason:     call.Reason,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		return connectorActionCallResult{}, err
	}

	principal, err := localExecutionPrincipal(runtime)
	if err != nil {
		return connectorActionCallResult{}, err
	}
	request, created, err := s.insertPreparedConnectorActionRequest(ctx, runtime, nil, prepared, connectors.ResultRunning, "", "", "", call.IdempotencyKey)
	if err != nil {
		return connectorActionCallResult{}, err
	}
	if !created {
		return replayedConnectorActionCallResult(request), nil
	}
	return s.executeInsertedConnectorAction(ctx, runtime, prepared, request, principal, connectorActionExecutionOptions{
		UnsupportedRunningError: "connector returned running for a local action that does not support asynchronous execution",
		ApprovalPendingError:    "connector returned approval_pending for a local operator action",
	})
}

func (s *Server) executeInsertedConnectorAction(
	ctx context.Context,
	runtime *databaseRuntime,
	prepared actions.PreparedRequest,
	request connectortargets.ActionRequest,
	principal executionprincipal.Principal,
	options connectorActionExecutionOptions,
) (connectorActionCallResult, error) {
	snapshot, err := s.snapshotPreparedConnectorAction(ctx, runtime, prepared)
	if err != nil {
		failureOutput := connectorActionFailureOutput(err)
		finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectors.ResultFailed, failureOutput, "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectorActionCallResult{}, finishErr
		}
		return connectorActionCallResult{Request: finished, Permission: options.Permission, Result: connectors.ActionResult{Status: finished.Status, Output: finished.Output, Error: finished.Error}}, nil
	}
	result, err := s.executePreparedConnectorAction(ctx, runtime, principal, prepared, snapshot)
	if err != nil {
		failureOutput := connectorActionFailureOutput(err)
		finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectorActionExecutionFailureStatus(err), failureOutput, "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectorActionCallResult{}, finishErr
		}
		return connectorActionCallResult{Request: finished, Permission: options.Permission, Result: connectors.ActionResult{Status: finished.Status, Output: finished.Output, Error: finished.Error}}, nil
	}
	status := result.Status
	if status == "" {
		status = connectors.ResultCompleted
	}
	request, err = s.captureConnectorActionSessionHandleIfReturned(ctx, runtime, request, result.Handles)
	if err != nil {
		finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectors.ResultOutcomeUnknown, nil, "", connectorActionHandleError, prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectorActionCallResult{}, errors.Join(err, finishErr)
		}
		return connectorActionCallResult{
			Request: finished, Permission: options.Permission,
			Result: connectors.ActionResult{Status: connectors.ResultOutcomeUnknown, Error: finished.Error},
		}, nil
	}
	if status == connectors.ResultRunning {
		if !s.connectorActionSupportsRunning(prepared) {
			finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectors.ResultError, nil, "", options.UnsupportedRunningError, prepared.ActionDefinition.OutputHint)
			if finishErr != nil {
				return connectorActionCallResult{}, finishErr
			}
			return connectorActionCallResult{
				Request:    finished,
				Permission: options.Permission,
				Result: connectors.ActionResult{
					Status: connectors.ResultError,
					Error:  options.UnsupportedRunningError,
				},
			}, nil
		}
		result.Handles.RequestID = request.ID
		if result.Handles.FollowupTool == "" {
			result.Handles.FollowupTool = options.FollowupTool
		}
		go s.finishActiveConnectorActionRequest(runtime, request.ID, prepared, principal, result.Handles)
		return connectorActionCallResult{Request: request, Permission: options.Permission, Result: result}, nil
	}
	if status == connectors.ResultApprovalPending {
		status = connectors.ResultFailed
		result.Error = options.ApprovalPendingError
	}
	finished, err := s.finishConnectorActionRequest(
		ctx, runtime, request.ID, status,
		result.Output, result.DisplayText, result.Error, prepared.ActionDefinition.OutputHint,
	)
	if err != nil {
		return connectorActionCallResult{}, err
	}
	result.Output = finished.Output
	result.DisplayText = finished.DisplayText
	result.Error = finished.Error
	result.Status = finished.Status
	return connectorActionCallResult{Request: finished, Permission: options.Permission, Result: result}, nil
}

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
	payload, err := runtime.vault.EncryptJSON(connectorActionExecutionEnvelope{
		Input:           prepared.Requested.Input,
		Payload:         prepared.Action.Payload,
		ApprovalPreview: prepared.Action.Preview,
		Reason:          prepared.Requested.Reason,
	})
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	identityHash, err := connectorActionIdempotencyIdentityHash(tokenID, prepared, idempotencyKey)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	insertInput := connectortargets.InsertActionRequestInput{
		TokenID:                 tokenID,
		TargetID:                prepared.Target.ID,
		ProfileID:               prepared.Profile.ID,
		ConnectorKind:           prepared.Target.ConnectorKind,
		ActionName:              prepared.Action.ActionName,
		Title:                   s.redactForPersistence(ctx, runtime, prepared.Action.Title),
		Summary:                 s.redactForPersistence(ctx, runtime, prepared.Action.Summary),
		Preview:                 redactedPreview,
		Source:                  prepared.Requested.Source,
		Input:                   redactedInput,
		EncryptedPayloadJSON:    payload,
		Reason:                  s.redactForPersistence(ctx, runtime, prepared.Requested.Reason),
		Status:                  status,
		ApprovalContext:         approvalContext,
		ApprovalContextHash:     approvalHash,
		IdempotencyKey:          strings.TrimSpace(idempotencyKey),
		IdempotencyIdentityHash: identityHash,
	}
	var request connectortargets.ActionRequest
	created := false
	err = s.withAuditedMutation(
		ctx, runtime, "gateway", tokenID, 0, "connector_action.request.created",
		func() any { return connectorActionRequestAuditPayload(request) },
		func(tx *sql.Tx) error {
			var err error
			request, created, err = connectortargets.NewTxStore(tx).InsertActionRequestIdempotent(ctx, insertInput)
			if err == nil && !created {
				return errAuditedMutationUnchanged
			}
			return err
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

func connectorActionIdempotencyIdentityHash(tokenID *int64, prepared actions.PreparedRequest, key string) (string, error) {
	input := prepared.IdempotencyInput
	if input == nil {
		input = prepared.Requested.Input
	}
	return connectorActionCallIdentityHash(
		tokenID,
		prepared.Requested.Source,
		connectortargets.ConnectorTargetRef(prepared.Target.ConnectorKind, prepared.Target.ID, prepared.Profile.ID),
		prepared.Action.ActionName,
		input,
		prepared.Requested.Reason,
		key,
	)
}

func connectorActionCallIdentityHash(tokenID *int64, source, targetRef, actionName string, input map[string]any, reason, key string) (string, error) {
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
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
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
	identityHash, err := connectorActionCallIdentityHash(tokenID, call.Source, call.TargetRef, call.ActionName, call.Input, call.Reason, key)
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

func (s *Server) snapshotPreparedConnectorAction(ctx context.Context, runtime *databaseRuntime, prepared actions.PreparedRequest) (connectorActionExecutionSnapshot, error) {
	profile, err := connectortargets.NewStore(runtime.database).GetCredentialProfile(ctx, prepared.Target.ID, prepared.Profile.ID)
	if err != nil {
		return connectorActionExecutionSnapshot{}, err
	}
	if current := connectortargets.CredentialProfileView(profile); !reflect.DeepEqual(current, prepared.Profile) {
		return connectorActionExecutionSnapshot{}, errors.New("connector credential profile changed after action preparation")
	}
	secrets := map[string]any{}
	if profile.EncryptedSecretJSON != "" {
		if err := runtime.vault.DecryptJSON(profile.EncryptedSecretJSON, &secrets); err != nil {
			return connectorActionExecutionSnapshot{}, err
		}
	}
	return connectorActionExecutionSnapshot{secrets: secrets}, nil
}

func (s *Server) executePreparedConnectorAction(ctx context.Context, runtime *databaseRuntime, principal executionprincipal.Principal, prepared actions.PreparedRequest, snapshot connectorActionExecutionSnapshot) (connectors.ActionResult, error) {
	if err := principal.Validate(); err != nil {
		return connectors.ActionResult{}, err
	}
	connector, ok := runtime.connectorRegistry().Get(prepared.Target.ConnectorKind)
	if !ok {
		return connectors.ActionResult{}, fmt.Errorf("connector not found: %s", prepared.Target.ConnectorKind)
	}
	result, err := connector.ExecuteAction(ctx, connectors.RuntimeContext{
		Target:       prepared.Target,
		Profile:      prepared.Profile,
		Secrets:      connectorSecretAccessor{values: snapshot.secrets},
		Events:       noopConnectorEventSink{},
		Principal:    principal,
		Capabilities: connectorRuntimeCapabilitiesFor(prepared.Target.ConnectorKind, s, runtime),
	}, prepared.Action)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if err := validateConnectorActionResult(result); err != nil {
		return connectors.ActionResult{}, err
	}
	redacted, err := s.redactConnectorActionResult(ctx, runtime, result, prepared.ActionDefinition.OutputHint)
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("process connector action result: %w", err)
	}
	return redacted, nil
}

func validateConnectorActionResult(result connectors.ActionResult) error {
	hasSessionID := result.Handles.SessionID > 0
	hasGeneration := result.Handles.SessionGeneration > 0
	if hasSessionID != hasGeneration {
		return errors.New("connector returned an incomplete session handle")
	}
	return nil
}

func connectorActionExecutionFailureStatus(err error) connectors.ResultStatus {
	switch connectors.ErrorStatus(err) {
	case connectors.ResultFailed, connectors.ResultError, connectors.ResultOutcomeUnknown:
		return connectors.ErrorStatus(err)
	}
	if errors.Is(err, actionresult.ErrInvalidOutput) {
		return connectors.ResultOutcomeUnknown
	}
	return connectors.ResultFailed
}

func (s *Server) finishActiveConnectorActionRequest(runtime *databaseRuntime, requestID int64, prepared actions.PreparedRequest, principal executionprincipal.Principal, handles connectors.ActionHandles) {
	adapter := s.connectorRuntimeAdapterFor(prepared.Target.ConnectorKind)
	if adapter == nil || !adapter.SupportsRunning(prepared) {
		return
	}
	adapter.FinishRunning(s, runtime, requestID, prepared, principal, handles)
}

func (s *Server) connectorActionSupportsRunning(prepared actions.PreparedRequest) bool {
	adapter := s.connectorRuntimeAdapterFor(prepared.Target.ConnectorKind)
	return adapter != nil && adapter.SupportsRunning(prepared)
}

func (s *Server) captureConnectorActionSessionHandle(ctx context.Context, runtime *databaseRuntime, requestID int64, handles connectors.ActionHandles) (connectortargets.ActionRequest, error) {
	captureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectorActionFinishTimeout)
	defer cancel()
	var request connectortargets.ActionRequest
	err := s.withAuditedTransaction(captureCtx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		var err error
		request, err = connectortargets.NewTxStore(tx).SetActionRequestSessionHandle(
			captureCtx, requestID, handles.SessionID, handles.SessionGeneration,
		)
		if err != nil {
			return err
		}
		return appendAudit(tx, "gateway", request.TokenID, 0, "connector_action.session_handle.updated", connectorActionRequestAuditPayload(request))
	})
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return request, nil
}

func (s *Server) captureConnectorActionSessionHandleIfReturned(
	ctx context.Context,
	runtime *databaseRuntime,
	request connectortargets.ActionRequest,
	handles connectors.ActionHandles,
) (connectortargets.ActionRequest, error) {
	hasSessionID := handles.SessionID > 0
	hasGeneration := handles.SessionGeneration > 0
	if !hasSessionID && !hasGeneration {
		return request, nil
	}
	if !hasSessionID || !hasGeneration {
		return connectortargets.ActionRequest{}, errors.New("connector returned an incomplete session handle")
	}
	return s.captureConnectorActionSessionHandle(ctx, runtime, request.ID, handles)
}

func (s *Server) finishConnectorActionRequest(ctx context.Context, runtime *databaseRuntime, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	return s.finishConnectorActionRequestWithAllowed(ctx, runtime, requestID, status, output, displayText, errorText, nil, hints...)
}

func (s *Server) finishConnectorActionRequestWithAllowed(ctx context.Context, runtime *databaseRuntime, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, allowedStatuses []connectors.ResultStatus, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectorActionFinishTimeout)
	defer cancel()
	redacted, err := s.redactConnectorActionResult(finishCtx, runtime, connectors.ActionResult{
		Output:      output,
		DisplayText: displayText,
		Error:       errorText,
	}, hints...)
	if err != nil {
		return connectortargets.ActionRequest{}, fmt.Errorf("process connector action result: %w", err)
	}
	var finished connectortargets.ActionRequest
	err = s.withAuditedTransaction(finishCtx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		var err error
		var changed bool
		finished, changed, err = connectortargets.NewTxStore(tx).FinishActionRequestWithChange(finishCtx, connectortargets.FinishActionRequestInput{
			ID: requestID, Status: status, Output: redacted.Output,
			DisplayText: redacted.DisplayText, Error: redacted.Error,
			AllowedStatuses: allowedStatuses,
		})
		if err == nil && !changed {
			return errAuditedMutationUnchanged
		}
		if err != nil {
			return err
		}
		return appendAudit(tx, "gateway", finished.TokenID, 0, "connector_action.request."+string(status), connectorActionRequestAuditPayload(finished))
	})
	if errors.Is(err, errAuditedMutationUnchanged) {
		return finished, nil
	}
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return finished, nil
}

func connectorActionRequestAuditPayload(request connectortargets.ActionRequest) map[string]any {
	return map[string]any{
		"request_id": request.ID, "target_id": request.TargetID, "profile_id": request.ProfileID,
		"connector_kind": request.ConnectorKind, "action_name": request.ActionName,
		"status": request.Status,
	}
}

func (s *Server) redactedConnectorValue(ctx context.Context, runtime *databaseRuntime, value any, sensitiveFields map[string]bool, capabilityFields map[string]bool) (any, error) {
	return actionresult.CanonicalizeAndRedact(value, actionresult.DefaultLimits(), actionresult.RedactionOptions{
		SensitiveField: func(key string) bool {
			return connectorOutputFieldSensitive(key, sensitiveFields)
		},
		TemporaryCapabilityField: func(key string) bool {
			return connectorOutputFieldDeclared(key, capabilityFields)
		},
		RedactText: func(value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		RedactCapability: func(value string) string {
			return s.redactCustom(ctx, runtime, value)
		},
	})
}

func (s *Server) redactConnectorActionResult(ctx context.Context, runtime *databaseRuntime, result connectors.ActionResult, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	sensitiveFields := connectorSensitiveOutputFields(hints...)
	capabilityFields := connectorTemporaryCapabilityFields(hints...)
	result.DisplayText = s.redactForPersistence(ctx, runtime, result.DisplayText)
	result.Error = s.redactForPersistence(ctx, runtime, result.Error)
	redacted, err := s.redactedConnectorValue(ctx, runtime, result.Output, sensitiveFields, capabilityFields)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	result.Output = redacted
	return result, nil
}

func (s *Server) redactConnectorActionInput(ctx context.Context, runtime *databaseRuntime, input map[string]any, sensitiveInputFields []string) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}
	fields := connectorSensitiveOutputFields()
	for _, field := range sensitiveInputFields {
		normalized := normalizeConnectorOutputField(field)
		if normalized != "" {
			fields[normalized] = true
		}
	}
	value, err := s.redactedConnectorValue(ctx, runtime, input, fields, nil)
	if err != nil {
		return nil, err
	}
	redacted, ok := value.(map[string]any)
	if !ok || redacted == nil {
		return nil, actionresult.ErrInvalidValue
	}
	return redacted, nil
}

func (s *Server) redactConnectorActionPreview(ctx context.Context, runtime *databaseRuntime, preview map[string]any, sensitiveFields []string, hints ...connectors.OutputHint) (map[string]any, error) {
	if preview == nil {
		return map[string]any{}, nil
	}
	fields := connectorSensitiveOutputFields(hints...)
	for _, field := range sensitiveFields {
		if normalized := normalizeConnectorOutputField(field); normalized != "" {
			fields[normalized] = true
		}
	}
	value, err := s.redactedConnectorValue(ctx, runtime, preview, fields, nil)
	if err != nil {
		return nil, err
	}
	redacted, ok := value.(map[string]any)
	if !ok || redacted == nil {
		return nil, actionresult.ErrInvalidValue
	}
	return redacted, nil
}

func connectorTemporaryCapabilityFields(hints ...connectors.OutputHint) map[string]bool {
	fields := map[string]bool{}
	for _, hint := range hints {
		for _, field := range hint.TemporaryCapabilityFields {
			if normalized := normalizeConnectorOutputField(field); normalized != "" {
				fields[normalized] = true
			}
		}
	}
	return fields
}

func connectorSensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	fields := map[string]bool{
		"api_key":           true,
		"api_token_hash":    true,
		"apikey":            true,
		"authorization":     true,
		"credential":        true,
		"credential_hash":   true,
		"credential_value":  true,
		"password":          true,
		"password_hash":     true,
		"private_key":       true,
		"refresh_token":     true,
		"secret":            true,
		"secret_access_key": true,
		"secret_hash":       true,
		"secret_value":      true,
		"token":             true,
		"token_hash":        true,
	}
	for _, hint := range hints {
		for _, field := range hint.SensitiveFields {
			normalized := normalizeConnectorOutputField(field)
			if normalized != "" {
				fields[normalized] = true
			}
		}
	}
	return fields
}

func connectorOutputFieldSensitive(key string, sensitiveFields map[string]bool) bool {
	normalized := normalizeConnectorOutputField(key)
	if normalized == "" {
		return false
	}
	if sensitiveFields[normalized] {
		return true
	}
	for field := range sensitiveFields {
		if strings.HasSuffix(normalized, "."+field) || strings.HasSuffix(normalized, "_"+field) {
			return true
		}
	}
	return false
}

func connectorOutputFieldDeclared(key string, fields map[string]bool) bool {
	normalized := normalizeConnectorOutputField(key)
	return normalized != "" && fields[normalized]
}

func normalizeConnectorOutputField(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func connectorApprovalContext(prepared actions.PreparedRequest, token tokens.Token, permission connectortargets.ActionPermission, capturedAt string) (string, string, error) {
	payloadHashMaterial, err := json.Marshal(map[string]any{
		"input":   prepared.Requested.Input,
		"payload": prepared.Action.Payload,
	})
	if err != nil {
		return "", "", err
	}
	actionDefinition := map[string]any{
		"name":                   prepared.ActionDefinition.Name,
		"label":                  prepared.ActionDefinition.Label,
		"description":            prepared.ActionDefinition.Description,
		"category":               prepared.ActionDefinition.Category,
		"risk":                   prepared.ActionDefinition.Risk,
		"input_schema":           prepared.ActionDefinition.InputSchema,
		"sensitive_input_fields": prepared.ActionDefinition.SensitiveInputFields,
		"output_hint":            prepared.ActionDefinition.OutputHint,
	}
	actionDefinitionHashMaterial, err := json.Marshal(actionDefinition)
	if err != nil {
		return "", "", err
	}
	contextMaterial, err := json.Marshal(prepared.Action.ContextMaterial)
	if err != nil {
		return "", "", err
	}
	dependencies := make([]map[string]any, 0, len(prepared.Dependencies))
	for _, dependency := range prepared.Dependencies {
		dependencies = append(dependencies, map[string]any{
			"purpose": dependency.Purpose,
			"target": map[string]any{
				"id":             dependency.Target.ID,
				"project_id":     dependency.Target.ProjectID,
				"ref":            dependency.Target.Ref,
				"connector_kind": dependency.Target.ConnectorKind,
				"name":           dependency.Target.Name,
				"config":         dependency.Target.Config,
				"updated_at":     dependency.Target.UpdatedAt,
			},
			"profile": map[string]any{
				"id":              dependency.Profile.ID,
				"kind":            dependency.Profile.Kind,
				"label":           dependency.Profile.Label,
				"risk_label":      dependency.Profile.RiskLabel,
				"updated_at":      dependency.Profile.UpdatedAt,
				"secret_revision": dependency.Profile.SecretRevision,
				"public":          dependency.Profile.Public,
			},
		})
	}
	snapshot := map[string]any{
		"schema_version": approvalContextSchemaVersion,
		"captured_at":    capturedAt,
		"tool": map[string]any{
			"name":           connectorActionToolName,
			"schema_version": approvalContextSchemaVersion,
		},
		"connector": map[string]any{
			"kind":    prepared.Target.ConnectorKind,
			"version": prepared.ConnectorVersion,
		},
		"token": map[string]any{
			"id":         token.ID,
			"expires_at": token.ExpiresAt,
			"revoked_at": token.RevokedAt,
		},
		"permission": map[string]any{
			"rule":       permission.ExecutionRule,
			"expires_at": permission.ExpiresAt,
		},
		"project": map[string]any{
			"id":   permission.ProjectID,
			"name": permission.ProjectName,
			"slug": permission.ProjectSlug,
		},
		"target": map[string]any{
			"id":             prepared.Target.ID,
			"ref":            prepared.Target.Ref,
			"connector_kind": prepared.Target.ConnectorKind,
			"name":           prepared.Target.Name,
			"config":         prepared.Target.Config,
			"updated_at":     prepared.Target.UpdatedAt,
		},
		"profile": map[string]any{
			"id":              prepared.Profile.ID,
			"kind":            prepared.Profile.Kind,
			"label":           prepared.Profile.Label,
			"risk_label":      prepared.Profile.RiskLabel,
			"updated_at":      prepared.Profile.UpdatedAt,
			"secret_revision": prepared.Profile.SecretRevision,
			"public":          prepared.Profile.Public,
		},
		"action": map[string]any{
			"name":            prepared.Action.ActionName,
			"risk":            prepared.Action.Risk,
			"definition":      actionDefinition,
			"definition_hash": sha256Hex(string(actionDefinitionHashMaterial)),
			"payload_hash":    sha256Hex(string(payloadHashMaterial)),
			"context_hash":    sha256Hex(string(contextMaterial)),
		},
		"dependencies": dependencies,
	}
	hash, err := hashGenericApprovalContext(snapshot)
	if err != nil {
		return "", "", err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", err
	}
	return string(payload), hash, nil
}

func hashGenericApprovalContext(snapshot map[string]any) (string, error) {
	clone := map[string]any{}
	for key, value := range snapshot {
		if key == "captured_at" {
			continue
		}
		clone[key] = value
	}
	payload, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	return sha256Hex(string(payload)), nil
}

func connectorApprovalDriftReason(previousContext string, currentContext string) string {
	var previous map[string]any
	var current map[string]any
	if err := json.Unmarshal([]byte(previousContext), &previous); err != nil {
		return "unknown"
	}
	if err := json.Unmarshal([]byte(currentContext), &current); err != nil {
		return "unknown"
	}
	for _, area := range []string{"connector", "token", "permission", "project", "target", "profile", "dependencies"} {
		if !reflect.DeepEqual(previous[area], current[area]) {
			return area
		}
	}
	if !reflect.DeepEqual(approvalActionValue(previous, "definition_hash"), approvalActionValue(current, "definition_hash")) {
		return "action_definition"
	}
	if !reflect.DeepEqual(approvalActionValue(previous, "payload_hash"), approvalActionValue(current, "payload_hash")) {
		return "payload"
	}
	if !reflect.DeepEqual(approvalActionValue(previous, "context_hash"), approvalActionValue(current, "context_hash")) {
		return "action_context"
	}
	if !reflect.DeepEqual(previous["action"], current["action"]) {
		return "action"
	}
	return "unknown"
}

func approvalActionValue(snapshot map[string]any, key string) any {
	action, _ := snapshot["action"].(map[string]any)
	if action == nil {
		return nil
	}
	return action[key]
}
