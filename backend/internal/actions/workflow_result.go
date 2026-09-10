package actions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func ExecutionFailureStatus(err error) connectors.ResultStatus {
	switch connectors.ErrorStatus(err) {
	case connectors.ResultFailed, connectors.ResultError, connectors.ResultOutcomeUnknown:
		return connectors.ErrorStatus(err)
	}
	if errors.Is(err, actionresult.ErrInvalidOutput) {
		return connectors.ResultOutcomeUnknown
	}
	return connectors.ResultFailed
}

func (r *Runtime) CaptureSessionHandle(ctx context.Context, requestID int64, handles connectors.ActionHandles) (connectortargets.ActionRequest, error) {
	captureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), actionFinishTimeout)
	defer cancel()
	var request connectortargets.ActionRequest
	err := r.mutations.WithTransaction(captureCtx, func(tx *sql.Tx, appendAudit AuditAppender) error {
		var err error
		request, err = connectortargets.NewTxStore(tx).SetActionRequestSessionHandle(captureCtx, requestID, handles.SessionID, handles.SessionGeneration)
		if err != nil {
			return err
		}
		return appendAudit(tx, "gateway", request.TokenID, 0, "connector_action.session_handle.updated", RequestAuditPayload(request))
	})
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return request, nil
}

func (r *Runtime) CaptureSessionHandleIfReturned(ctx context.Context, request connectortargets.ActionRequest, handles connectors.ActionHandles) (connectortargets.ActionRequest, error) {
	hasSessionID := handles.SessionID > 0
	hasGeneration := handles.SessionGeneration > 0
	if !hasSessionID && !hasGeneration {
		return request, nil
	}
	if !hasSessionID || !hasGeneration {
		return connectortargets.ActionRequest{}, errors.New("connector returned an incomplete session handle")
	}
	return r.CaptureSessionHandle(ctx, request.ID, handles)
}

func (r *Runtime) Finish(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	return r.FinishAllowed(ctx, requestID, status, output, displayText, errorText, nil, hints...)
}

func (r *Runtime) FinishAllowed(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, allowedStatuses []connectors.ResultStatus, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), actionFinishTimeout)
	defer cancel()
	boundary, err := r.CredentialBoundaryForRequest(finishCtx, requestID)
	if err != nil {
		return connectortargets.ActionRequest{}, fmt.Errorf("load connector credential redaction boundary: %w", err)
	}
	redacted, err := r.redactor.ResultWithCredentialBoundary(finishCtx, connectors.ActionResult{
		Output: output, DisplayText: displayText, Error: errorText,
	}, boundary, hints...)
	if err != nil {
		return connectortargets.ActionRequest{}, fmt.Errorf("process connector action result: %w", err)
	}
	var finished connectortargets.ActionRequest
	for attempt := 0; attempt < actionFinishAttempts; attempt++ {
		finished, err = r.persistFinish(finishCtx, requestID, status, redacted, allowedStatuses)
		if err == nil {
			return finished, nil
		}
		if finishCtx.Err() != nil || attempt == actionFinishAttempts-1 {
			break
		}
		timer := time.NewTimer(actionFinishRetryDelay * time.Duration(attempt+1))
		select {
		case <-finishCtx.Done():
			timer.Stop()
			return connectortargets.ActionRequest{}, errors.Join(err, finishCtx.Err())
		case <-timer.C:
		}
	}
	return connectortargets.ActionRequest{}, fmt.Errorf("persist connector action terminal state after %d attempts: %w", actionFinishAttempts, err)
}

func (r *Runtime) persistFinish(ctx context.Context, requestID int64, status connectors.ResultStatus, result connectors.ActionResult, allowedStatuses []connectors.ResultStatus) (connectortargets.ActionRequest, error) {
	var finished connectortargets.ActionRequest
	err := r.mutations.WithTransaction(ctx, func(tx *sql.Tx, appendAudit AuditAppender) error {
		var err error
		var changed bool
		finished, changed, err = connectortargets.NewTxStore(tx).FinishActionRequestWithChange(ctx, connectortargets.FinishActionRequestInput{
			ID: requestID, Status: status, Output: result.Output,
			DisplayText: result.DisplayText, Error: result.Error, AllowedStatuses: allowedStatuses,
		})
		if err == nil && !changed {
			return ErrMutationUnchanged
		}
		if err != nil {
			return err
		}
		return appendAudit(tx, "gateway", finished.TokenID, 0, "connector_action.request."+string(status), RequestAuditPayload(finished))
	})
	if errors.Is(err, ErrMutationUnchanged) {
		return finished, nil
	}
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return finished, nil
}
