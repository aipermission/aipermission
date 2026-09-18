package actions

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type dispatchCompletionStage uint8

const (
	dispatchFinished dispatchCompletionStage = iota
	dispatchRunning
	dispatchUnsupportedRunning
	dispatchFinishedWithoutObservation
)

type dispatchCompletion struct {
	request             connectortargets.ActionRequest
	result              connectors.ActionResult
	stage               dispatchCompletionStage
	boundaryTransferred bool
}

// The caller owns admission and approval checks; only post-dispatch state is shared.
func (r *Runtime) completeDispatch(
	ctx, finishCtx, shutdownFinishCtx context.Context,
	request connectortargets.ActionRequest,
	prepared PreparedRequest,
	principal executionprincipal.Principal,
	snapshot ExecutionSnapshot,
	options ExecutionOptions,
) (dispatchCompletion, error) {
	requestID := request.ID
	finish := func(status connectors.ResultStatus, output any, displayText, errorText string) (connectortargets.ActionRequest, error) {
		return r.Finish(finishCtx, requestID, status, output, displayText, errorText, prepared.ActionDefinition.OutputHint)
	}
	result, err := r.ExecutePrepared(ctx, principal, prepared, snapshot)
	if err != nil {
		finished, finishErr := finish(ExecutionFailureStatus(err), FailureOutput(err), "", err.Error())
		if finishErr != nil {
			return dispatchCompletion{}, NewTerminalPersistenceError(requestID, finishErr)
		}
		return dispatchCompletion{request: finished, result: connectors.ActionResult{Status: finished.Status, Output: finished.Output, Error: finished.Error}, stage: dispatchFinishedWithoutObservation}, nil
	}
	captured, err := r.CaptureSessionHandleIfReturned(ctx, request, result.Handles)
	if err != nil {
		finished, finishErr := finish(connectors.ResultOutcomeUnknown, nil, "", HandlePersistenceError)
		if finishErr != nil {
			return dispatchCompletion{}, NewTerminalPersistenceError(requestID, errors.Join(err, finishErr))
		}
		return dispatchCompletion{request: finished, result: connectors.ActionResult{Status: finished.Status, Error: finished.Error}, stage: dispatchFinishedWithoutObservation}, nil
	}
	request = captured
	if result.Status == connectors.ResultRunning {
		if !r.runningActions.SupportsRunning(prepared) {
			finished, finishErr := finish(connectors.ResultError, nil, "", options.UnsupportedRunningError)
			if finishErr != nil {
				return dispatchCompletion{}, NewTerminalPersistenceError(requestID, finishErr)
			}
			return dispatchCompletion{request: finished, result: connectors.ActionResult{Status: finished.Status, Error: finished.Error}, stage: dispatchUnsupportedRunning}, nil
		}
		result.Handles.RequestID = requestID
		if result.Handles.FollowupTool == "" {
			result.Handles.FollowupTool = options.FollowupTool
		}
		launched := r.launchFinalizer(func(finalizerCtx context.Context) {
			defer r.ClearCredentialBoundary(requestID)
			r.runningActions.FinishRunning(finalizerCtx, requestID, prepared, principal, result.Handles)
		})
		if !launched {
			finished, finishErr := r.Finish(shutdownFinishCtx, requestID, connectors.ResultOutcomeUnknown, nil, "", "connector action runtime is shutting down", prepared.ActionDefinition.OutputHint)
			if finishErr != nil {
				return dispatchCompletion{}, NewTerminalPersistenceError(requestID, finishErr)
			}
			return dispatchCompletion{request: finished, result: connectors.ActionResult{Status: finished.Status, Error: finished.Error}, stage: dispatchFinishedWithoutObservation}, nil
		}
		return dispatchCompletion{request: request, result: result, stage: dispatchRunning, boundaryTransferred: true}, nil
	}
	status := result.Status
	if status == connectors.ResultApprovalPending {
		status = connectors.ResultFailed
		result.Error = options.ApprovalPendingError
	}
	finished, err := finish(status, result.Output, result.DisplayText, result.Error)
	if err != nil {
		return dispatchCompletion{}, NewTerminalPersistenceError(requestID, err)
	}
	result.Output, result.DisplayText, result.Error, result.Status = finished.Output, finished.DisplayText, finished.Error, finished.Status
	return dispatchCompletion{request: finished, result: result, stage: dispatchFinished}, nil
}
