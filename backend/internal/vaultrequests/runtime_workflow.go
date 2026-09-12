package vaultrequests

import (
	"context"
	"database/sql"
)

func (r *Runtime) workflowPorts(actor, userNote, startedAction, finishedActionPrefix string) WorkflowPorts {
	return WorkflowPorts{
		Claim: func(ctx context.Context, id int64) (Request, error) {
			return r.claim(ctx, id, actor, startedAction, userNote)
		},
		Execute: r.execute,
		ExecuteAtomic: func(ctx context.Context, request Request) (WorkflowResult, bool, error) {
			if r.executeAtomic == nil {
				return WorkflowResult{}, false, nil
			}
			return r.executeAtomic(ctx, request, actor, userNote, finishedActionPrefix)
		},
		FinalizationTimeout: r.executionTimeout,
		Complete: func(ctx context.Context, id int64, status string, output any, errorText string) (Request, error) {
			return r.complete(ctx, id, status, output, errorText, userNote, actor, finishedActionPrefix+"."+status)
		},
		Get:         r.store.Get,
		Repair:      r.repairProjection,
		Compensate:  r.compensate,
		RedactError: r.redactError,
		IsStale:     r.isStale,
	}
}

func (r *Runtime) claim(ctx context.Context, requestID int64, actor, action, userNote string) (Request, error) {
	tokenID, runtimeID := r.auditIdentity(ctx, requestID)
	var item Request
	err := r.mutations.WithMutation(
		ctx, actor, tokenID, runtimeID, action,
		func() any { return RequestAuditPayload(item, userNote) },
		func(tx *sql.Tx) error {
			var claimErr error
			item, claimErr = NewTxStore(tx).Claim(ctx, requestID)
			return claimErr
		},
	)
	return item, err
}

func (r *Runtime) complete(
	ctx context.Context,
	requestID int64,
	status string,
	output any,
	errorText string,
	userNote string,
	actor string,
	action string,
) (Request, error) {
	tokenID, runtimeID := r.auditIdentity(ctx, requestID)
	var item Request
	err := r.mutations.WithMutation(
		ctx, actor, tokenID, runtimeID, action,
		func() any { return RequestAuditPayload(item, userNote) },
		func(tx *sql.Tx) error {
			var completeErr error
			item, completeErr = NewTxStore(tx).Complete(ctx, requestID, status, output, errorText, userNote)
			return completeErr
		},
	)
	return item, err
}

func (r *Runtime) auditIdentity(ctx context.Context, requestID int64) (*int64, int64) {
	if requestID < 1 {
		return nil, 0
	}
	item, err := r.store.getRaw(ctx, requestID)
	if err != nil {
		return nil, 0
	}
	return &item.TokenID, valueOrZero(item.RuntimeID)
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (r *Runtime) StalePendingForContext(ctx context.Context, itemID, bindingID int64, reason string) error {
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.StalePendingForContext(ctx, itemID, bindingID, reason)
}

func (r *Runtime) StalePendingForProject(ctx context.Context, projectID int64, reason string) error {
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.StalePendingForProject(ctx, projectID, reason)
}

func (r *Runtime) StalePendingForRuntimes(ctx context.Context, runtimeIDs []int64, reason string) error {
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.StalePendingForRuntimes(ctx, runtimeIDs, reason)
}

func (r *Runtime) StalePendingForAction(ctx context.Context, actionName, reason string) error {
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.StalePendingForAction(ctx, actionName, reason)
}

func (r *Runtime) FailRunning(ctx context.Context, reason string) error {
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.FailRunning(ctx, reason)
}
