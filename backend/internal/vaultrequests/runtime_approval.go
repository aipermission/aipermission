package vaultrequests

import (
	"context"
	"database/sql"
	"strings"
)

func (r *Runtime) List(ctx context.Context, status string, limit int) ([]Request, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r.store.List(ctx, strings.TrimSpace(status), limit)
}

func (r *Runtime) Get(ctx context.Context, id int64) (Request, error) {
	if err := r.validate(); err != nil {
		return Request{}, err
	}
	return r.store.Get(ctx, id)
}

func (r *Runtime) RunPending(ctx context.Context, id int64, userNote string) (WorkflowResult, error) {
	if err := r.validate(); err != nil {
		return WorkflowResult{}, err
	}
	if !r.mcpStarted() {
		return WorkflowResult{}, ErrMCPExecutionStopped
	}
	userNote, err := normalizeUserNote(userNote)
	if err != nil {
		return WorkflowResult{}, err
	}
	executionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.executionTimeout)
	defer cancel()
	return RunWorkflow(executionCtx, id, r.workflowPorts("user", userNote, "vault.action.run_requested", "vault.action"))
}

func (r *Runtime) DeclinePending(ctx context.Context, id int64, userNote string) (Request, error) {
	if err := r.validate(); err != nil {
		return Request{}, err
	}
	userNote, err := normalizeUserNote(userNote)
	if err != nil {
		return Request{}, err
	}
	tokenID, runtimeID := r.auditIdentity(ctx, id)
	var item Request
	err = r.mutations.WithMutation(
		ctx, "user", tokenID, runtimeID, "vault.action.declined",
		func() any { return RequestAuditPayload(item, userNote) },
		func(tx *sql.Tx) error {
			var declineErr error
			item, declineErr = NewTxStore(tx).Decline(ctx, id, userNote)
			return declineErr
		},
	)
	return item, err
}

func (r *Runtime) CancelOwned(ctx context.Context, id, tokenID int64) (Request, error) {
	if err := r.validate(); err != nil {
		return Request{}, err
	}
	var item Request
	err := r.mutations.WithMutation(
		ctx, "mcp", &tokenID, 0, "mcp.vault_action.canceled",
		func() any { return RequestAuditPayload(item, "") },
		func(tx *sql.Tx) error {
			var cancelErr error
			item, cancelErr = NewTxStore(tx).CancelOwned(ctx, id, tokenID)
			return cancelErr
		},
	)
	return item, err
}

func normalizeUserNote(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len([]byte(value)) > maxUserNoteBytes {
		return "", ValidationError("user_note must be 8192 bytes or less")
	}
	return value, nil
}

func RequestAuditPayload(item Request, userNote string) map[string]any {
	return map[string]any{
		"request_id": item.ID, "project_id": item.ProjectID, "action_name": item.ActionName,
		"approval_context_hash": item.ApprovalContextHash, "note": strings.TrimSpace(userNote) != "",
	}
}
