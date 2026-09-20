package vaultrequests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type CallInput struct {
	TokenID        int64
	ProjectRef     string
	ActionName     string
	Input          map[string]any
	Reason         string
	IdempotencyKey string
}

type RequestView struct {
	Request          Request
	OutputAuthorized bool
}

func (r *Runtime) Call(ctx context.Context, input CallInput) (Request, error) {
	if err := r.validate(); err != nil {
		return Request{}, err
	}
	input.ProjectRef = strings.TrimSpace(input.ProjectRef)
	input.ActionName = strings.TrimSpace(input.ActionName)
	input.Reason = strings.TrimSpace(input.Reason)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.TokenID < 1 || input.ProjectRef == "" || input.IdempotencyKey == "" {
		return Request{}, ValidationError("project_ref and idempotency_key are required")
	}
	if input.Reason == "" {
		return Request{}, ValidationError("reason is required")
	}
	if len([]byte(input.IdempotencyKey)) > maxIdempotencyKeyBytes {
		return Request{}, ValidationError("idempotency_key is too long")
	}
	if len([]byte(input.Reason)) > maxReasonBytes {
		return Request{}, ValidationError(fmt.Sprintf("reason must be %d bytes or less", maxReasonBytes))
	}
	normalizedInput, err := NormalizeActionInput(input.ActionName, input.Input)
	if err != nil {
		return Request{}, ValidationError(err.Error())
	}
	releaseDelivery, err := r.acquireDelivery(ctx)
	if err != nil {
		return Request{}, err
	}
	deliveryHeld := true
	defer func() {
		if deliveryHeld {
			releaseDelivery()
		}
	}()
	existing, err := r.store.GetByIdempotencyKey(ctx, input.TokenID, input.IdempotencyKey)
	if err == nil {
		exact, openErr := r.exactRequest(existing)
		if openErr != nil {
			return Request{}, openErr
		}
		projectID, resolveErr := r.resolveProject(ctx, input.ProjectRef)
		if errors.Is(resolveErr, ErrProjectNotFound) && storedProjectReferenceMatches(exact, input.ProjectRef) {
			projectID, resolveErr = exact.ProjectID, nil
		}
		if resolveErr != nil {
			return Request{}, resolveErr
		}
		if !SameActionCall(exact, projectID, input.ActionName, normalizedInput, input.Reason) {
			return Request{}, ErrIdempotencyConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Request{}, err
	}
	projectID, err := r.resolveProject(ctx, input.ProjectRef)
	if err != nil {
		return Request{}, err
	}
	if !r.allowRequest(input.TokenID) {
		return Request{}, ErrRequestRateLimited
	}
	prepared, err := r.prepare(ctx, input.TokenID, input.ProjectRef, input.ActionName, normalizedInput)
	if err != nil {
		return Request{}, err
	}
	if prepared.ProjectID != projectID {
		return Request{}, ErrProjectNotFound
	}
	contextMap, err := approvalContextMap(prepared.ApprovalContext)
	if err != nil {
		return Request{}, err
	}
	runtimeID := (*int64)(nil)
	if prepared.RuntimeID > 0 {
		runtimeID = &prepared.RuntimeID
	}
	initialStatus := StatusApprovalPending
	if prepared.RunImmediately {
		initialStatus = StatusRunning
	}
	publicInput, publicReason, err := r.publicProjection(ctx, prepared.Input, input.Reason)
	if err != nil {
		return Request{}, err
	}
	envelope := ExecutionEnvelope{
		Input: prepared.Input, Reason: input.Reason, ApprovalContext: contextMap,
	}
	createInput := CreateInput{
		TokenID: input.TokenID, ProjectID: prepared.ProjectID, RuntimeID: runtimeID,
		ActionName: input.ActionName, Input: publicInput, Reason: publicReason,
		ApprovalContext: contextMap, ApprovalContextHash: prepared.ApprovalContextHash,
		IdempotencyKey: input.IdempotencyKey, InitialStatus: initialStatus,
	}
	var request Request
	created := false
	err = r.mutations.WithMutation(
		ctx, "mcp", &input.TokenID, prepared.RuntimeID, "mcp.vault_action.request.created",
		func() any { return RequestAuditPayload(request, "") },
		func(tx *sql.Tx) error {
			var createErr error
			request, created, createErr = NewTxStore(tx).CreateSealed(ctx, createInput, func(requestID int64) (string, error) {
				return r.sealRequest(requestID, envelope)
			})
			if createErr == nil && !created {
				exact, openErr := r.exactRequest(request)
				if openErr != nil {
					return openErr
				}
				if !SameActionCall(exact, projectID, input.ActionName, normalizedInput, input.Reason) {
					return ErrIdempotencyConflict
				}
			}
			if createErr == nil && !created {
				return errMutationUnchanged
			}
			return createErr
		},
	)
	if errors.Is(err, errMutationUnchanged) {
		return request, nil
	}
	if err != nil {
		return Request{}, err
	}
	if !prepared.RunImmediately {
		r.mutations.Observe(ctx, "mcp", &input.TokenID, prepared.RuntimeID, "mcp.vault_action.approval_pending", map[string]any{
			"request_id": request.ID, "project_id": prepared.ProjectID, "action_name": request.ActionName,
			"approval_context_hash": request.ApprovalContextHash,
		})
		return request, nil
	}
	releaseDelivery()
	deliveryHeld = false
	executionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.executionTimeout)
	defer cancel()
	exact := request
	exact.Input = envelope.Input
	exact.Reason = envelope.Reason
	exact.ApprovalContext = envelope.ApprovalContext
	result, err := RunClaimedWorkflow(executionCtx, exact, r.workflowPorts("mcp", "", "", "mcp.vault_action"))
	if err != nil {
		return Request{}, err
	}
	return result.Request, nil
}

func storedProjectReferenceMatches(request Request, ref string) bool {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "id:") {
		id, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(ref, "id:")), 10, 64)
		return err == nil && id > 0 && id == request.ProjectID
	}
	if strings.HasPrefix(ref, "slug:") {
		return strings.TrimSpace(strings.TrimPrefix(ref, "slug:")) == request.ProjectSlug
	}
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil && id > 0 && id == request.ProjectID {
		return true
	}
	return ref != "" && ref == request.ProjectSlug
}

func (r *Runtime) DeliverOwned(ctx context.Context, id, tokenID int64, deliver func(RequestView)) error {
	return r.deliverOwned(ctx, id, tokenID, true, deliver)
}

func (r *Runtime) DeliverCallResult(ctx context.Context, id, tokenID int64, deliver func(RequestView)) error {
	return r.deliverOwned(ctx, id, tokenID, false, deliver)
}

func (r *Runtime) deliverOwned(ctx context.Context, id, tokenID int64, stalePending bool, deliver func(RequestView)) error {
	if err := r.validate(); err != nil {
		return err
	}
	if deliver == nil {
		return ErrRuntimeUnavailable
	}
	releaseDelivery, err := r.acquireDelivery(ctx)
	if err != nil {
		return err
	}
	defer releaseDelivery()
	item, err := r.store.Get(ctx, id)
	if errors.Is(err, ErrNotFound) || (err == nil && item.TokenID != tokenID) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	exact, openErr := r.exactRequest(item)
	authorized := openErr == nil && r.authorizeOutput(ctx, exact)
	if stalePending && item.Status == StatusApprovalPending && !authorized {
		if stale, staleErr := r.store.StalePending(ctx, item.ID, "Vault approval context changed; send a fresh request"); staleErr == nil {
			item = stale
		}
	}
	deliver(RequestView{Request: item, OutputAuthorized: authorized})
	return nil
}

func SameActionCall(request Request, projectID int64, actionName string, input map[string]any, reason string) bool {
	if request.ProjectID != projectID || request.ActionName != actionName || request.Reason != reason {
		return false
	}
	requestJSON, requestErr := json.Marshal(request.Input)
	inputJSON, inputErr := json.Marshal(input)
	return requestErr == nil && inputErr == nil && string(requestJSON) == string(inputJSON)
}

func approvalContextMap(approval ApprovalContext) (map[string]any, error) {
	payload, err := json.Marshal(approval)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}
