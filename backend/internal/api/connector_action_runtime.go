package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

var errMCPExecutionStopped = errors.New("MCP execution is stopped")
var errConnectorAuthorizationChanged = errors.New("connector authorization changed before dispatch")

const (
	connectorActionToolName          = "connector.call_action"
	connectorActionApprovalHint      = "Wait 3 seconds, then poll this connector action request until it is completed, failed, declined, stale, blocked, or outcome_unknown."
	connectorActionHandleError       = "connector action returned a session handle that could not be persisted; inspect the target state before retrying because the remote outcome is unknown"
	connectorActionRunningHint       = "Wait 3 seconds, then call get_connector_action_request again. Use the connector-specific read or recovery actions when the connector exposes them."
	connectorActionMissingPermission = "This token is not allowed to run this connector action for the selected target/profile"
	connectorActionFinishTimeout     = 10 * time.Second
	connectorActionFinishAttempts    = 3
	connectorActionFinishRetryDelay  = 50 * time.Millisecond
	connectorActionPersistenceError  = "connector action may have been dispatched, but its final state could not be persisted; inspect request history before retrying"
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
	RequiredPermissionRule  connectortargets.ActionPermissionRule
	UnsupportedRunningError string
	ApprovalPendingError    string
	FollowupTool            string
}

type connectorActionTerminalPersistenceError struct {
	RequestID int64
	Err       error
}

func (err *connectorActionTerminalPersistenceError) Error() string {
	return connectorActionPersistenceError
}

func (err *connectorActionTerminalPersistenceError) Unwrap() error {
	return err.Err
}

func newConnectorActionTerminalPersistenceError(requestID int64, err error) error {
	if err == nil {
		return nil
	}
	return &connectorActionTerminalPersistenceError{RequestID: requestID, Err: err}
}

type connectorActionExecutionEnvelope struct {
	Input                map[string]any `json:"input"`
	Payload              map[string]any `json:"payload"`
	ApprovalPreview      map[string]any `json:"approval_preview,omitempty"`
	SensitiveInputFields []string       `json:"sensitive_input_fields,omitempty"`
	Reason               string         `json:"reason,omitempty"`
}

type connectorSecretAccessor struct {
	values   map[string]any
	boundary connectorCredentialBoundary
}

type connectorActionExecutionSnapshot struct {
	secrets            map[string]any
	credentialBoundary connectorCredentialBoundary
}

func (a connectorSecretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := a.values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	switch typed := value.(type) {
	case string:
		a.boundary.Add(typed)
		return typed, nil
	default:
		text := fmt.Sprint(typed)
		a.boundary.Add(text)
		return text, nil
	}
}

func (a connectorSecretAccessor) RegisterSensitiveValue(value string) {
	a.boundary.Add(value)
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
	var replay connectorActionCallResult
	var replayed bool
	var err error
	if call.Source == commandRequestSourceMCP {
		release, acquireErr := runtime.vaultDelivery.acquireDelivery(ctx)
		if acquireErr != nil {
			return connectorActionCallResult{}, acquireErr
		}
		if !runtime.isMCPStarted() {
			release()
			return connectorActionCallResult{}, errMCPExecutionStopped
		}
		replay, replayed, err = replayConnectorActionCall(ctx, runtime, &tokenID, call)
		release()
	} else {
		replay, replayed, err = replayConnectorActionCall(ctx, runtime, &tokenID, call)
	}
	if err != nil {
		return connectorActionCallResult{}, err
	}
	if replayed {
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
	if call.Source == commandRequestSourceMCP && call.IdempotencyKey == "" && prepared.Action.Risk != connectors.RiskRead {
		return connectorActionCallResult{}, errors.New("idempotency_key is required for connector mutations; update the MCP client and retry with a caller-stable key")
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
		RequiredPermissionRule:  connectortargets.ActionPermissionAlwaysRun,
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
	if call.IdempotencyKey == "" && prepared.Action.Risk != connectors.RiskRead {
		return connectorActionCallResult{}, errors.New("idempotency_key is required for local connector mutations")
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
	release, err := runtime.vaultDelivery.acquireDelivery(ctx)
	if err != nil {
		return connectorActionCallResult{}, err
	}
	claimHeld := true
	defer func() {
		if claimHeld {
			release()
		}
	}()
	prepared, err = s.revalidatePreparedConnectorAction(ctx, runtime, request, prepared, options.RequiredPermissionRule)
	if err != nil {
		finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectors.ResultFailed, nil, "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectorActionCallResult{}, finishErr
		}
		return connectorActionCallResult{Request: finished, Permission: options.Permission, Result: connectors.ActionResult{Status: finished.Status, Error: finished.Error}}, nil
	}
	snapshot, err := s.snapshotPreparedConnectorAction(ctx, runtime, prepared)
	if err != nil {
		failureOutput := connectorActionFailureOutput(err)
		finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectors.ResultFailed, failureOutput, "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectorActionCallResult{}, finishErr
		}
		return connectorActionCallResult{Request: finished, Permission: options.Permission, Result: connectors.ActionResult{Status: finished.Status, Output: finished.Output, Error: finished.Error}}, nil
	}
	runtime.setConnectorCredentialBoundary(request.ID, snapshot.credentialBoundary)
	clearCredentialBoundary := true
	defer func() {
		if clearCredentialBoundary {
			runtime.clearConnectorCredentialBoundary(request.ID)
		}
	}()
	claimed, dispatched, err := s.beginConnectorActionDispatch(ctx, runtime, request.ID)
	if err != nil {
		return connectorActionCallResult{}, err
	}
	if !dispatched {
		return connectorActionCallResult{
			Request: claimed, Permission: options.Permission,
			Result: connectors.ActionResult{Status: claimed.Status, Output: claimed.Output, DisplayText: claimed.DisplayText, Error: claimed.Error},
		}, nil
	}
	release()
	claimHeld = false
	request = claimed
	result, err := s.executePreparedConnectorAction(ctx, runtime, principal, prepared, snapshot)
	if err != nil {
		failureOutput := connectorActionFailureOutput(err)
		finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectorActionExecutionFailureStatus(err), failureOutput, "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectorActionCallResult{}, newConnectorActionTerminalPersistenceError(request.ID, finishErr)
		}
		return connectorActionCallResult{Request: finished, Permission: options.Permission, Result: connectors.ActionResult{Status: finished.Status, Output: finished.Output, Error: finished.Error}}, nil
	}
	status := result.Status
	request, err = s.captureConnectorActionSessionHandleIfReturned(ctx, runtime, request, result.Handles)
	if err != nil {
		finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectors.ResultOutcomeUnknown, nil, "", connectorActionHandleError, prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectorActionCallResult{}, newConnectorActionTerminalPersistenceError(request.ID, errors.Join(err, finishErr))
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
				return connectorActionCallResult{}, newConnectorActionTerminalPersistenceError(request.ID, finishErr)
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
		clearCredentialBoundary = false
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
		return connectorActionCallResult{}, newConnectorActionTerminalPersistenceError(request.ID, err)
	}
	result.Output = finished.Output
	result.DisplayText = finished.DisplayText
	result.Error = finished.Error
	result.Status = finished.Status
	return connectorActionCallResult{Request: finished, Permission: options.Permission, Result: result}, nil
}

func (s *Server) revalidatePreparedConnectorAction(
	ctx context.Context,
	runtime *databaseRuntime,
	request connectortargets.ActionRequest,
	prepared actions.PreparedRequest,
	requiredRule connectortargets.ActionPermissionRule,
) (actions.PreparedRequest, error) {
	if request.TokenID != nil && request.Source == commandRequestSourceMCP && !runtime.isMCPStarted() {
		return prepared, fmt.Errorf("%w: MCP execution is stopped", errConnectorAuthorizationChanged)
	}
	fresh, err := runtime.prepareConnectorAction(ctx, actions.PrepareRequest{
		Source: prepared.Requested.Source, TargetRef: prepared.Requested.TargetRef,
		ActionName: prepared.Requested.ActionName, Input: prepared.Requested.Input,
		Reason: prepared.Requested.Reason, CreatedAt: prepared.Requested.CreatedAt,
	})
	if err != nil {
		return prepared, fmt.Errorf("%w: target, profile, or action is no longer available", errConnectorAuthorizationChanged)
	}
	if request.TokenID == nil {
		return fresh, nil
	}
	token, err := runtime.tokens.Get(ctx, *request.TokenID)
	if err != nil || token.RevokedAt != "" || expired(token.ExpiresAt, time.Now().UTC()) {
		return prepared, fmt.Errorf("%w: token is no longer active", errConnectorAuthorizationChanged)
	}
	permission, err := connectortargets.NewStore(runtime.database).GetActionPermission(
		ctx, token.ID, fresh.Target.ID, fresh.Profile.ID, fresh.Action.ActionName, time.Now().UTC(),
	)
	if errors.Is(err, connectortargets.ErrActionPermissionNotFound) || (err == nil && permission.ExecutionRule != requiredRule) {
		return prepared, fmt.Errorf("%w: permission rule changed", errConnectorAuthorizationChanged)
	}
	if err != nil {
		return prepared, err
	}
	_, currentHash, err := connectorApprovalContext(fresh, token, permission, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return prepared, err
	}
	if request.ApprovalContextHash == "" || request.ApprovalContextHash != currentHash {
		return prepared, fmt.Errorf("%w: approval context changed", errConnectorAuthorizationChanged)
	}
	return fresh, nil
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
		if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, profile.EncryptedSecretJSON, &secrets); err != nil {
			return connectorActionExecutionSnapshot{}, err
		}
	}
	boundary := newConnectorCredentialBoundary(secrets)
	boundary.Add(connectorActionSensitiveValues(
		prepared.Requested.Input,
		prepared.Action.Payload,
		prepared.ActionDefinition.SensitiveInputFields,
	)...)
	return connectorActionExecutionSnapshot{secrets: secrets, credentialBoundary: boundary}, nil
}

func (s *Server) executePreparedConnectorAction(ctx context.Context, runtime *databaseRuntime, principal executionprincipal.Principal, prepared actions.PreparedRequest, snapshot connectorActionExecutionSnapshot) (connectors.ActionResult, error) {
	service := actions.NewService(runtime.connectorRegistry(), connectortargets.NewResolver(runtime.database))
	result, err := service.Execute(ctx, actions.ExecutionRequest{
		Prepared: prepared,
		Runtime: connectors.RuntimeContext{
			Target:       prepared.Target,
			Profile:      prepared.Profile,
			Secrets:      connectorSecretAccessor{values: snapshot.secrets, boundary: snapshot.credentialBoundary},
			Events:       noopConnectorEventSink{},
			Principal:    principal,
			Capabilities: connectorRuntimeCapabilitiesForAction(prepared.Target.ConnectorKind, s, runtime, prepared.Dependencies),
		},
	})
	if err != nil {
		return connectors.ActionResult{}, err
	}
	redacted, err := s.redactConnectorActionResultWithCredentialBoundary(ctx, runtime, result, snapshot.credentialBoundary, prepared.ActionDefinition.OutputHint)
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("process connector action result: %w", err)
	}
	return redacted, nil
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
	defer runtime.clearConnectorCredentialBoundary(requestID)
	adapter := s.connectorRuntimeAdapterFor(prepared.Target.ConnectorKind)
	if adapter == nil || !adapter.SupportsRunning(prepared) {
		return
	}
	gatewayPort, runtimePort := newActionFinishPorts(s, runtime, prepared.Target.ConnectorKind)
	if err := adapter.FinishRunning(gatewayPort, runtimePort, requestID, prepared, principal, handles); err != nil {
		log.Printf("finish running connector action failed connector=%q request=%d error=%v", prepared.Target.ConnectorKind, requestID, err)
	}
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
	boundary, err := connectorCredentialBoundaryForActionRequest(finishCtx, runtime, requestID)
	if err != nil {
		return connectortargets.ActionRequest{}, fmt.Errorf("load connector credential redaction boundary: %w", err)
	}
	redacted, err := s.redactConnectorActionResultWithCredentialBoundary(finishCtx, runtime, connectors.ActionResult{
		Output:      output,
		DisplayText: displayText,
		Error:       errorText,
	}, boundary, hints...)
	if err != nil {
		return connectortargets.ActionRequest{}, fmt.Errorf("process connector action result: %w", err)
	}
	var finished connectortargets.ActionRequest
	for attempt := 0; attempt < connectorActionFinishAttempts; attempt++ {
		finished, err = s.persistConnectorActionFinish(finishCtx, runtime, requestID, status, redacted, allowedStatuses)
		if err == nil {
			return finished, nil
		}
		if finishCtx.Err() != nil || attempt == connectorActionFinishAttempts-1 {
			break
		}
		timer := time.NewTimer(connectorActionFinishRetryDelay * time.Duration(attempt+1))
		select {
		case <-finishCtx.Done():
			timer.Stop()
			return connectortargets.ActionRequest{}, errors.Join(err, finishCtx.Err())
		case <-timer.C:
		}
	}
	return connectortargets.ActionRequest{}, fmt.Errorf("persist connector action terminal state after %d attempts: %w", connectorActionFinishAttempts, err)
}

func (s *Server) persistConnectorActionFinish(ctx context.Context, runtime *databaseRuntime, requestID int64, status connectors.ResultStatus, result connectors.ActionResult, allowedStatuses []connectors.ResultStatus) (connectortargets.ActionRequest, error) {
	var finished connectortargets.ActionRequest
	err := s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		var err error
		var changed bool
		finished, changed, err = connectortargets.NewTxStore(tx).FinishActionRequestWithChange(ctx, connectortargets.FinishActionRequestInput{
			ID: requestID, Status: status, Output: result.Output,
			DisplayText: result.DisplayText, Error: result.Error,
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

func connectorCredentialBoundaryForActionRequest(ctx context.Context, runtime *databaseRuntime, requestID int64) (connectorCredentialBoundary, error) {
	if runtime == nil || runtime.database == nil || runtime.vault == nil {
		return connectorCredentialBoundary{}, errors.New("connector runtime is unavailable")
	}
	if boundary, ok := runtime.connectorCredentialBoundary(requestID); ok {
		return boundary, nil
	}
	store := connectortargets.NewStore(runtime.database)
	request, err := store.GetActionRequest(ctx, requestID)
	if err != nil {
		return connectorCredentialBoundary{}, err
	}
	profile, err := store.GetCredentialProfile(ctx, request.TargetID, request.ProfileID)
	if err != nil {
		return connectorCredentialBoundary{}, err
	}
	secrets := map[string]any{}
	if profile.EncryptedSecretJSON != "" {
		if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, profile.EncryptedSecretJSON, &secrets); err != nil {
			return connectorCredentialBoundary{}, err
		}
	}
	boundary := newConnectorCredentialBoundary(secrets)
	if strings.TrimSpace(request.EncryptedPayloadJSON) == "" {
		return boundary, nil
	}
	var envelope connectorActionExecutionEnvelope
	if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorActionRequest, request.ID, request.EncryptedPayloadJSON, &envelope); err != nil {
		return connectorCredentialBoundary{}, fmt.Errorf("decrypt connector action redaction boundary: %w", err)
	}
	boundary.Add(connectorActionSensitiveValues(envelope.Input, envelope.Payload, envelope.SensitiveInputFields)...)
	boundary.Add(connectorActionSensitiveValues(envelope.ApprovalPreview, nil, envelope.SensitiveInputFields)...)
	return boundary, nil
}

func (rt *databaseRuntime) setConnectorCredentialBoundary(requestID int64, boundary connectorCredentialBoundary) {
	if rt == nil || requestID < 1 {
		return
	}
	rt.credBoundaryMu.Lock()
	if rt.credBoundaries == nil {
		rt.credBoundaries = map[int64]connectorCredentialBoundary{}
	}
	rt.credBoundaries[requestID] = boundary
	rt.credBoundaryMu.Unlock()
}

func (rt *databaseRuntime) connectorCredentialBoundary(requestID int64) (connectorCredentialBoundary, bool) {
	if rt == nil || requestID < 1 {
		return connectorCredentialBoundary{}, false
	}
	rt.credBoundaryMu.RLock()
	boundary, ok := rt.credBoundaries[requestID]
	rt.credBoundaryMu.RUnlock()
	return boundary, ok
}

func (rt *databaseRuntime) clearConnectorCredentialBoundary(requestID int64) {
	if rt == nil || requestID < 1 {
		return
	}
	rt.credBoundaryMu.Lock()
	delete(rt.credBoundaries, requestID)
	rt.credBoundaryMu.Unlock()
}
