package actions

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type secretAccessor struct {
	values   map[string]any
	boundary actionresult.CredentialBoundary
}

func (a secretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := a.values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	text := fmt.Sprint(value)
	a.boundary.Add(text)
	return text, nil
}

func (a secretAccessor) RegisterSensitiveValue(value string) { a.boundary.Add(value) }

type noopEventSink struct{}

func (noopEventSink) Emit(context.Context, connectors.ActionEvent) error { return nil }

func (r *Runtime) Call(ctx context.Context, call Call) (CallResult, error) {
	if err := r.validate(); err != nil {
		return CallResult{}, err
	}
	if call.TokenID < 1 {
		return CallResult{}, fmt.Errorf("token_id is required")
	}
	if call.Source == "" {
		call.Source = SourceMCP
	}
	tokenID := call.TokenID
	var replay CallResult
	var replayed bool
	var err error
	if call.Source == SourceMCP {
		release, acquireErr := r.delivery.Acquire(ctx)
		if acquireErr != nil {
			return CallResult{}, acquireErr
		}
		if !r.mcpStarted() {
			release()
			return CallResult{}, ErrMCPExecutionStopped
		}
		replay, replayed, err = r.Replay(ctx, &tokenID, call)
		release()
	} else {
		replay, replayed, err = r.Replay(ctx, &tokenID, call)
	}
	if err != nil {
		return CallResult{}, err
	}
	if replayed {
		return replay, nil
	}
	prepared, err := r.service.Prepare(ctx, PrepareRequest{
		Source: call.Source, TargetRef: call.TargetRef, ActionName: call.ActionName,
		Input: call.Input, Reason: call.Reason, CreatedAt: r.now().UTC(),
	})
	if err != nil {
		return CallResult{}, err
	}
	if call.Source == SourceMCP && call.IdempotencyKey == "" && prepared.Action.Risk != connectors.RiskRead {
		return CallResult{}, errors.New("idempotency_key is required for connector mutations; update the MCP client and retry with a caller-stable key")
	}
	store := connectortargets.NewStore(r.database)
	permission, err := store.GetActionPermission(ctx, call.TokenID, prepared.Target.ID, prepared.Profile.ID, prepared.Action.ActionName, r.now().UTC())
	if errors.Is(err, connectortargets.ErrActionPermissionNotFound) {
		request, created, insertErr := r.InsertTokenRequestWithCapacity(ctx, call.TokenID, prepared, connectortargets.ActionPermission{}, connectors.ResultBlocked, MissingPermissionError, call.IdempotencyKey)
		if insertErr != nil {
			return CallResult{}, insertErr
		}
		if !created {
			return replayedCallResult(request), nil
		}
		return CallResult{Request: request, Result: connectors.ActionResult{Status: connectors.ResultBlocked, Error: MissingPermissionError}}, nil
	}
	if err != nil {
		return CallResult{}, err
	}
	if permission.ExecutionRule == connectortargets.ActionPermissionBlocked {
		const blocked = "Connector action is blocked for this token"
		request, created, insertErr := r.InsertTokenRequestWithCapacity(ctx, call.TokenID, prepared, permission, connectors.ResultBlocked, blocked, call.IdempotencyKey)
		if insertErr != nil {
			return CallResult{}, insertErr
		}
		if !created {
			return replayedCallResult(request), nil
		}
		return CallResult{Request: request, Permission: permission, Result: connectors.ActionResult{Status: connectors.ResultBlocked, Error: blocked}}, nil
	}
	if permission.ExecutionRule == connectortargets.ActionPermissionApprovalRequired {
		request, created, insertErr := r.InsertTokenRequestWithCapacity(ctx, call.TokenID, prepared, permission, connectors.ResultApprovalPending, "", call.IdempotencyKey)
		if insertErr != nil {
			return CallResult{}, insertErr
		}
		if !created {
			return replayedCallResult(request), nil
		}
		return CallResult{Request: request, Permission: permission, Result: connectors.ActionResult{
			Status: connectors.ResultApprovalPending, Error: "Waiting for user approval.",
			Handles: connectors.ActionHandles{RequestID: request.ID, FollowupTool: "get_connector_action_request"},
		}}, nil
	}
	principal, err := r.tokenPrincipal(call.TokenID)
	if err != nil {
		return CallResult{}, err
	}
	request, created, err := r.InsertTokenRequestWithCapacity(ctx, call.TokenID, prepared, permission, connectors.ResultRunning, "", call.IdempotencyKey)
	if err != nil {
		return CallResult{}, err
	}
	if !created {
		return replayedCallResult(request), nil
	}
	return r.ExecuteInserted(ctx, prepared, request, principal, ExecutionOptions{
		Permission: permission, RequiredPermissionRule: connectortargets.ActionPermissionAlwaysRun,
		UnsupportedRunningError: "connector returned running for an action that does not support asynchronous execution",
		ApprovalPendingError:    "connector returned approval_pending after execution was already allowed",
		FollowupTool:            "get_connector_action_request",
	})
}

func (r *Runtime) RunLocal(ctx context.Context, call Call) (CallResult, error) {
	if err := r.validate(); err != nil {
		return CallResult{}, err
	}
	if call.Source == "" {
		call.Source = SourceManual
	}
	if replay, ok, err := r.Replay(ctx, nil, call); err != nil {
		return CallResult{}, err
	} else if ok {
		return replay, nil
	}
	prepared, err := r.service.Prepare(ctx, PrepareRequest{
		Source: call.Source, TargetRef: call.TargetRef, ActionName: call.ActionName,
		Input: call.Input, Reason: call.Reason, CreatedAt: r.now().UTC(),
	})
	if err != nil {
		return CallResult{}, err
	}
	if call.IdempotencyKey == "" && prepared.Action.Risk != connectors.RiskRead {
		return CallResult{}, errors.New("idempotency_key is required for local connector mutations")
	}
	principal, err := r.localPrincipal()
	if err != nil {
		return CallResult{}, err
	}
	request, created, err := r.InsertPreparedRequest(ctx, nil, prepared, connectors.ResultRunning, "", "", "", call.IdempotencyKey)
	if err != nil {
		return CallResult{}, err
	}
	if !created {
		return replayedCallResult(request), nil
	}
	return r.ExecuteInserted(ctx, prepared, request, principal, ExecutionOptions{
		UnsupportedRunningError: "connector returned running for a local action that does not support asynchronous execution",
		ApprovalPendingError:    "connector returned approval_pending for a local operator action",
	})
}

func (r *Runtime) ExecuteInserted(ctx context.Context, prepared PreparedRequest, request connectortargets.ActionRequest, principal executionprincipal.Principal, options ExecutionOptions) (CallResult, error) {
	release, err := r.delivery.Acquire(ctx)
	if err != nil {
		return CallResult{}, err
	}
	claimHeld := true
	defer func() {
		if claimHeld {
			release()
		}
	}()
	prepared, err = r.Revalidate(ctx, request, prepared, options.RequiredPermissionRule)
	if err != nil {
		finished, finishErr := r.Finish(ctx, request.ID, connectors.ResultFailed, nil, "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return CallResult{}, finishErr
		}
		return CallResult{Request: finished, Permission: options.Permission, Result: connectors.ActionResult{Status: finished.Status, Error: finished.Error}}, nil
	}
	snapshot, err := r.Snapshot(ctx, prepared)
	if err != nil {
		finished, finishErr := r.Finish(ctx, request.ID, connectors.ResultFailed, FailureOutput(err), "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return CallResult{}, finishErr
		}
		return CallResult{Request: finished, Permission: options.Permission, Result: connectors.ActionResult{Status: finished.Status, Output: finished.Output, Error: finished.Error}}, nil
	}
	r.TrackCredentialBoundary(request.ID, snapshot.CredentialBoundary)
	clearBoundary := true
	defer func() {
		if clearBoundary {
			r.ClearCredentialBoundary(request.ID)
		}
	}()
	claimed, dispatched, err := r.BeginDispatch(ctx, request.ID)
	if err != nil {
		return CallResult{}, err
	}
	if !dispatched {
		return CallResult{Request: claimed, Permission: options.Permission, Result: connectors.ActionResult{
			Status: claimed.Status, Output: claimed.Output, DisplayText: claimed.DisplayText, Error: claimed.Error,
		}}, nil
	}
	release()
	claimHeld = false
	request = claimed
	completed, err := r.completeDispatch(ctx, ctx, ctx, request, prepared, principal, snapshot, options)
	if err != nil {
		return CallResult{}, err
	}
	if completed.boundaryTransferred {
		clearBoundary = false
	}
	return CallResult{Request: completed.request, Permission: options.Permission, Result: completed.result}, nil
}

func (r *Runtime) Revalidate(ctx context.Context, request connectortargets.ActionRequest, prepared PreparedRequest, requiredRule connectortargets.ActionPermissionRule) (PreparedRequest, error) {
	if request.TokenID != nil && request.Source == SourceMCP && !r.mcpStarted() {
		return prepared, fmt.Errorf("%w: MCP execution is stopped", ErrConnectorAuthorizationChanged)
	}
	fresh, err := r.service.Prepare(ctx, prepared.Requested)
	if err != nil {
		return prepared, fmt.Errorf("%w: target, profile, or action is no longer available", ErrConnectorAuthorizationChanged)
	}
	if request.TokenID == nil {
		return fresh, nil
	}
	token, err := r.tokens.Get(ctx, *request.TokenID, r.now().UTC())
	if err != nil || !token.Active {
		return prepared, fmt.Errorf("%w: token is no longer active", ErrConnectorAuthorizationChanged)
	}
	permission, err := connectortargets.NewStore(r.database).GetActionPermission(ctx, token.ID, fresh.Target.ID, fresh.Profile.ID, fresh.Action.ActionName, r.now().UTC())
	if errors.Is(err, connectortargets.ErrActionPermissionNotFound) || (err == nil && permission.ExecutionRule != requiredRule) {
		return prepared, fmt.Errorf("%w: permission rule changed", ErrConnectorAuthorizationChanged)
	}
	if err != nil {
		return prepared, err
	}
	tokenSnapshot, permissionSnapshot := approvalSnapshots(token, permission)
	_, currentHash, err := BuildApprovalContext(fresh, tokenSnapshot, permissionSnapshot, r.now().UTC().Format(time.RFC3339))
	if err != nil {
		return prepared, err
	}
	if request.ApprovalContextHash == "" || request.ApprovalContextHash != currentHash {
		return prepared, fmt.Errorf("%w: approval context changed", ErrConnectorAuthorizationChanged)
	}
	return fresh, nil
}

func (r *Runtime) Snapshot(ctx context.Context, prepared PreparedRequest) (ExecutionSnapshot, error) {
	profile, err := connectortargets.NewStore(r.database).GetCredentialProfile(ctx, prepared.Target.ID, prepared.Profile.ID)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	if current := connectortargets.CredentialProfileView(profile); !reflect.DeepEqual(current, prepared.Profile) {
		return ExecutionSnapshot{}, errors.New("connector credential profile changed after action preparation")
	}
	secrets, err := r.sealedRecords.OpenCredentialProfile(profile.ID, profile.EncryptedSecretJSON)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	boundary := actionresult.NewCredentialBoundary(secrets)
	boundary.Add(actionresult.SensitiveValues(prepared.Requested.Input, prepared.Action.Payload, prepared.ActionDefinition.SensitiveInputFields)...)
	return ExecutionSnapshot{Secrets: secrets, CredentialBoundary: boundary}, nil
}

func (r *Runtime) ExecutePrepared(ctx context.Context, principal executionprincipal.Principal, prepared PreparedRequest, snapshot ExecutionSnapshot) (connectors.ActionResult, error) {
	result, err := r.service.Execute(ctx, ExecutionRequest{Prepared: prepared, Runtime: connectors.RuntimeContext{
		Target: prepared.Target, Profile: prepared.Profile,
		Secrets: secretAccessor{values: snapshot.Secrets, boundary: snapshot.CredentialBoundary},
		Events:  noopEventSink{}, Principal: connectors.Principal{
			Kind: connectors.PrincipalKind(principal.Kind), TokenID: principal.TokenID,
			WorkspaceID: principal.WorkspaceID, RuntimeInstanceID: principal.RuntimeInstanceID,
		},
		Capabilities: r.capabilities(prepared.Target.ConnectorKind, prepared.Dependencies),
	}})
	if err != nil {
		return connectors.ActionResult{}, err
	}
	redacted, err := r.redactor.ResultWithCredentialBoundary(ctx, result, snapshot.CredentialBoundary, prepared.ActionDefinition.OutputHint)
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("process connector action result: %w", err)
	}
	return redacted, nil
}

func (r *Runtime) tokenPrincipal(tokenID int64) (executionprincipal.Principal, error) {
	workspaceID, runtimeInstanceID, err := r.runtimeIdentity()
	if err != nil {
		return executionprincipal.Principal{}, err
	}
	return executionprincipal.MCPToken(tokenID, workspaceID, runtimeInstanceID)
}

func (r *Runtime) localPrincipal() (executionprincipal.Principal, error) {
	workspaceID, runtimeInstanceID, err := r.runtimeIdentity()
	if err != nil {
		return executionprincipal.Principal{}, err
	}
	return executionprincipal.LocalOperator(workspaceID, runtimeInstanceID)
}

func approvalSnapshots(token AuthorizationToken, permission connectortargets.ActionPermission) (ApprovalTokenSnapshot, ApprovalPermissionSnapshot) {
	return ApprovalTokenSnapshot{ID: token.ID, ExpiresAt: token.ExpiresAt, RevokedAt: token.RevokedAt}, ApprovalPermissionSnapshot{
		Rule: string(permission.ExecutionRule), ExpiresAt: permission.ExpiresAt,
		ProjectID: permission.ProjectID, ProjectName: permission.ProjectName, ProjectSlug: permission.ProjectSlug,
	}
}
