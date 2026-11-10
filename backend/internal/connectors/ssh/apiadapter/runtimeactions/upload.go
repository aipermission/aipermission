package runtimeactions

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func (e runtimeExecutor) launchFileUpload(ctx context.Context, runtime connectors.RuntimeContext, runtimeID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	gateway, ok := e.server.(connectorapi.FileUploadGateway)
	if !ok {
		return connectors.ActionResult{}, errors.New("SSH upload gateway is unavailable")
	}
	ref := uploadReference(runtime, action)
	status, err := gateway.ConnectorLaunchFileUpload(ctx, connectorapi.FileUploadDispatch{
		FileUploadReference: ref, RuntimeID: runtimeID, Authorization: connectorapi.TransferAuthorizationFor(runtime),
		Manifest: connectors.FileUploadManifestFromInput(action.Payload),
	})
	return uploadResult(ref.Ref, status, "Check get_file_upload_status before treating this upload as completed.", err)
}

func (e runtimeExecutor) fileUploadStatus(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	gateway, ok := e.server.(connectorapi.FileUploadGateway)
	if !ok {
		return connectors.ActionResult{}, errors.New("SSH upload gateway is unavailable")
	}
	ref := uploadReference(runtime, action)
	status, err := gateway.ConnectorFileUploadStatus(ctx, ref)
	return uploadResult(ref.Ref, status, "", err)
}

func uploadReference(runtime connectors.RuntimeContext, action connectors.PreparedAction) connectorapi.FileUploadReference {
	return connectorapi.FileUploadReference{
		Principal: connectorPrincipal(runtime.Principal), TargetRef: action.TargetRef,
		Ref: stringPayload(action.Payload, "upload_ref"), UploadAction: sshconnector.ActionUploadFile,
	}
}

func uploadResult(ref string, status connectorapi.FileUploadStatus, message string, err error) (connectors.ActionResult, error) {
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted, DisplayText: message,
		Output: map[string]any{
			"upload_ref": ref, "status": status.Status, "transfer_id": status.TransferID,
			"transferred_bytes": status.TransferredBytes, "failure_kind": status.FailureKind,
			"action_request_id": status.ActionRequestID,
		},
	}, nil
}
