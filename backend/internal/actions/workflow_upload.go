package actions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (r *Runtime) pendingUploadReady(ctx context.Context, request connectortargets.ActionRequest, targetRef string) (bool, error) {
	if request.TokenID == nil || r.uploadReady == nil {
		return false, nil
	}
	return r.uploadReady(ctx, connectors.FileUploadApproval{
		TokenID: *request.TokenID, RequestID: request.ID, TargetRef: targetRef,
		ActionName: request.ActionName, Ref: request.IdempotencyKey,
	})
}

// RefreshRequest reconciles expiring upload approvals before returning a polled
// request. The caller must check token ownership before invoking this workflow.
func (r *Runtime) RefreshRequest(ctx context.Context, request connectortargets.ActionRequest) (connectortargets.ActionRequest, error) {
	if !pendingUploadApproval(request) {
		return request, nil
	}
	if err := r.validate(); err != nil {
		return request, err
	}
	release, err := r.delivery.Acquire(ctx)
	if err != nil {
		return request, err
	}
	defer release()
	return r.refreshPendingUpload(ctx, request, connectors.FormatTargetRef(request.ConnectorKind, request.TargetID, request.ProfileID))
}

func pendingUploadApproval(request connectortargets.ActionRequest) bool {
	if request.Status != connectors.ResultApprovalPending || request.TokenID == nil || request.Source != SourceMCP {
		return false
	}
	var snapshot struct {
		Action struct {
			Definition struct {
				MaxUploadBytes int64 `json:"max_upload_bytes"`
			} `json:"definition"`
		} `json:"action"`
	}
	return json.Unmarshal([]byte(request.ApprovalContext), &snapshot) == nil && snapshot.Action.Definition.MaxUploadBytes > 0
}

func (r *Runtime) refreshUploadReplay(ctx context.Context, request connectortargets.ActionRequest, targetRef string) (connectortargets.ActionRequest, error) {
	if !pendingUploadApproval(request) {
		return request, nil
	}
	return r.refreshPendingUpload(ctx, request, targetRef)
}

func (r *Runtime) refreshPendingUpload(ctx context.Context, request connectortargets.ActionRequest, targetRef string) (connectortargets.ActionRequest, error) {
	ready, err := r.pendingUploadReady(ctx, request, targetRef)
	if err != nil || ready {
		return request, err
	}
	return r.finishStaleApproval(ctx, request.ID, connectors.ErrUploadStageExpired.Error(), "upload_stage", []connectors.ResultStatus{connectors.ResultApprovalPending})
}

func (r *Runtime) withUploadDispatch(ctx context.Context, request connectortargets.ActionRequest, prepared PreparedRequest, options ExecutionOptions) context.Context {
	if prepared.ActionDefinition.MaxUploadBytes <= 0 || request.Source != SourceMCP ||
		request.TokenID == nil || request.IdempotencyKey == "" {
		return ctx
	}
	expiry, err := uploadGrantExpiry(request.ApprovalContext)
	return connectors.WithUploadDispatch(ctx, request.IdempotencyKey, func(ctx context.Context) (context.Context, func(), error) {
		if err != nil {
			return nil, nil, ErrConnectorAuthorizationChanged
		}
		cancel := func() {}
		if !expiry.IsZero() {
			ctx, cancel = context.WithDeadline(ctx, expiry)
		}
		release, err := r.delivery.Acquire(ctx)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		if err := r.revalidateUploadDispatch(ctx, request, prepared, options.RequiredPermissionRule); err != nil {
			release()
			cancel()
			return nil, nil, err
		}
		return ctx, func() { release(); cancel() }, nil
	})
}

func uploadGrantExpiry(approval string) (time.Time, error) {
	type grant struct {
		ExpiresAt string `json:"expires_at"`
	}
	var snapshot struct {
		Token      grant `json:"token"`
		Permission grant `json:"permission"`
	}
	if err := json.Unmarshal([]byte(approval), &snapshot); err != nil {
		return time.Time{}, err
	}
	var earliest time.Time
	for _, expires := range []string{snapshot.Token.ExpiresAt, snapshot.Permission.ExpiresAt} {
		if expires == "" {
			continue
		}
		deadline, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			return time.Time{}, err
		}
		if earliest.IsZero() || deadline.Before(earliest) {
			earliest = deadline
		}
	}
	return earliest, nil
}

func (r *Runtime) revalidateUploadDispatch(ctx context.Context, original connectortargets.ActionRequest, prepared PreparedRequest, rule connectortargets.ActionPermissionRule) error {
	current, err := connectortargets.NewStore(r.database).GetActionRequest(ctx, original.ID)
	if err != nil || current.ApprovalContextHash != original.ApprovalContextHash ||
		(current.Status != connectors.ResultRunning && current.Status != connectors.ResultCompleted) {
		return ErrConnectorAuthorizationChanged
	}
	_, err = r.Revalidate(ctx, original, prepared, rule)
	return err
}
