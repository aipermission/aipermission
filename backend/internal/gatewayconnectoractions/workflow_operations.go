package gatewayconnectoractions

import (
	"context"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type RequestPersistence interface {
	InsertTokenRequest(context.Context, int64, actions.PreparedRequest, connectortargets.ActionPermission, connectors.ResultStatus, string, string) (connectortargets.ActionRequest, bool, error)
	InsertPreparedRequest(context.Context, *int64, actions.PreparedRequest, connectors.ResultStatus, string, string, string, string) (connectortargets.ActionRequest, bool, error)
}

type DispatchWorkflow interface {
	ExecuteInserted(context.Context, actions.PreparedRequest, connectortargets.ActionRequest, executionprincipal.Principal, actions.ExecutionOptions) (actions.CallResult, error)
	Snapshot(context.Context, actions.PreparedRequest) (actions.ExecutionSnapshot, error)
	CaptureSessionHandleIfReturned(context.Context, connectortargets.ActionRequest, connectors.ActionHandles) (connectortargets.ActionRequest, error)
	BeginDispatch(context.Context, int64) (connectortargets.ActionRequest, bool, error)
}

type RecoveryWorkflow interface {
	CredentialBoundaryForRequest(context.Context, int64) (actions.CredentialBoundary, error)
	TrackCredentialBoundary(int64, actions.CredentialBoundary)
	CredentialBoundary(int64) (actions.CredentialBoundary, bool)
	Recover(context.Context, time.Time)
	PersistExpiredRecovery(context.Context, int64, time.Time) (connectortargets.ActionRequest, error)
}

type requestPersistence struct{ workflow *workflowHandle }

func (scope requestPersistence) InsertTokenRequest(ctx context.Context, tokenID int64, prepared actions.PreparedRequest, permission connectortargets.ActionPermission, status connectors.ResultStatus, errorText, idempotencyKey string) (connectortargets.ActionRequest, bool, error) {
	return scope.workflow.runtime.InsertTokenRequest(ctx, tokenID, prepared, permission, status, errorText, idempotencyKey)
}

func (scope requestPersistence) InsertPreparedRequest(ctx context.Context, tokenID *int64, prepared actions.PreparedRequest, status connectors.ResultStatus, errorText, approvalContext, approvalHash, idempotencyKey string) (connectortargets.ActionRequest, bool, error) {
	return scope.workflow.runtime.InsertPreparedRequest(ctx, tokenID, prepared, status, errorText, approvalContext, approvalHash, idempotencyKey)
}

type dispatchWorkflow struct{ workflow *workflowHandle }

func (scope dispatchWorkflow) ExecuteInserted(ctx context.Context, prepared actions.PreparedRequest, request connectortargets.ActionRequest, principal executionprincipal.Principal, options actions.ExecutionOptions) (actions.CallResult, error) {
	return scope.workflow.runtime.ExecuteInserted(ctx, prepared, request, principal, options)
}

func (scope dispatchWorkflow) Snapshot(ctx context.Context, prepared actions.PreparedRequest) (actions.ExecutionSnapshot, error) {
	return scope.workflow.runtime.Snapshot(ctx, prepared)
}

func (scope dispatchWorkflow) CaptureSessionHandleIfReturned(ctx context.Context, request connectortargets.ActionRequest, handles connectors.ActionHandles) (connectortargets.ActionRequest, error) {
	return scope.workflow.runtime.CaptureSessionHandleIfReturned(ctx, request, handles)
}

func (scope dispatchWorkflow) BeginDispatch(ctx context.Context, requestID int64) (connectortargets.ActionRequest, bool, error) {
	return scope.workflow.runtime.BeginDispatch(ctx, requestID)
}

type recoveryWorkflow struct{ workflow *workflowHandle }

func (scope recoveryWorkflow) CredentialBoundaryForRequest(ctx context.Context, requestID int64) (actions.CredentialBoundary, error) {
	return scope.workflow.runtime.CredentialBoundaryForRequest(ctx, requestID)
}

func (scope recoveryWorkflow) TrackCredentialBoundary(requestID int64, boundary actions.CredentialBoundary) {
	scope.workflow.runtime.TrackCredentialBoundary(requestID, boundary)
}

func (scope recoveryWorkflow) CredentialBoundary(requestID int64) (actions.CredentialBoundary, bool) {
	return scope.workflow.runtime.CredentialBoundary(requestID)
}

func (scope recoveryWorkflow) Recover(ctx context.Context, now time.Time) {
	scope.workflow.runtime.Recover(ctx, now)
}

func (scope recoveryWorkflow) PersistExpiredRecovery(ctx context.Context, requestID int64, now time.Time) (connectortargets.ActionRequest, error) {
	return scope.workflow.runtime.PersistExpiredRecovery(ctx, requestID, now)
}

func (component *Component) Persistence(workspace Workspace) (RequestPersistence, error) {
	workflow, err := component.workflow(workspace)
	if err != nil {
		return nil, err
	}
	return requestPersistence{workflow: workflow}, nil
}

func (component *Component) Dispatch(workspace Workspace) (DispatchWorkflow, error) {
	workflow, err := component.workflow(workspace)
	if err != nil {
		return nil, err
	}
	return dispatchWorkflow{workflow: workflow}, nil
}

func (component *Component) Recovery(workspace Workspace) (RecoveryWorkflow, error) {
	workflow, err := component.workflow(workspace)
	if err != nil {
		return nil, err
	}
	return recoveryWorkflow{workflow: workflow}, nil
}
