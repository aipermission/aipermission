package gatewaytransfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func (uploads WorkspaceUploads) Stage(ctx context.Context, tokenID int64, request connectors.FileUploadStageRequest, reader io.Reader) error {
	component := uploads.component
	if component == nil || component.runner == nil {
		return errors.New("upload workspace is unavailable")
	}
	runtime, err := component.runtimes.RuntimeForWorkspace(uploads.workspace)
	if err != nil {
		return err
	}
	identity := filetransfer.UploadIdentity{TargetRef: request.TargetRef, ActionName: request.ActionName, Manifest: filetransfer.UploadManifest(request.Manifest)}
	if _, err := filetransfer.MCPUploadClaim(tokenID, request.Ref, identity); err != nil {
		return fmt.Errorf("%w: %v", connectors.ErrInvalidUploadManifest, err)
	}
	expected := request.Manifest.ContentIdentity()
	// One extra byte distinguishes truncation from a body larger than its manifest.
	staged, size, digest, err := component.runner.StageUploadFile(runtime, io.LimitReader(reader, expected.SizeBytes+1))
	if err != nil {
		return err
	}
	owned := false
	defer func() {
		if !owned {
			component.runner.RemoveTempPath(runtime, staged)
		}
	}()
	if err := expected.Verify(size, digest); err != nil {
		return err
	}
	// The multipart reader checks EOF after its file part before returning EOF.
	_, owned, err = runtime.Storage().CreateMCPUpload(ctx, filetransfer.MCPUploadStage{
		TokenID: tokenID, IdempotencyKey: request.Ref, Identity: identity, RuntimeID: request.RuntimeID, TempPath: staged,
	}, time.Now())
	if errors.Is(err, filetransfer.ErrMCPUploadCapacity) {
		return connectors.ErrUploadStagingCapacity
	}
	if errors.Is(err, filetransfer.ErrIdempotencyConflict) || errors.Is(err, filetransfer.ErrIdempotencyResultExpired) {
		return connectors.ErrUploadStageConflict
	}
	return err
}

func (component *Component) LaunchMCPUpload(ctx context.Context, workspace Workspace, handlers *FileTransferHTTPHandlers, request connectorapi.FileUploadDispatch) (connectorapi.FileUploadStatus, error) {
	authorize := connectors.UploadDispatchAuthorization(ctx, request.Ref)
	if !validUploadPrincipal(workspace, request.Principal) || authorize == nil {
		return connectorapi.FileUploadStatus{}, filetransfer.ErrNotFound
	}
	runtime, err := component.runtimes.RuntimeForWorkspace(workspace)
	if err != nil {
		return connectorapi.FileUploadStatus{}, err
	}
	item, err := runtime.Storage().GetMCPUpload(ctx, request.Principal.TokenID, request.Ref, filetransfer.UploadIdentity{
		TargetRef: request.TargetRef, ActionName: request.UploadAction, Manifest: filetransfer.UploadManifest(request.Manifest),
	})
	if err != nil {
		return connectorapi.FileUploadStatus{}, err
	}
	if item.RuntimeID != request.RuntimeID || request.TargetRef != request.Authorization.TargetRef {
		return connectorapi.FileUploadStatus{}, filetransfer.ErrNotFound
	}
	if !filetransfer.MCPUploadReady(item, time.Now()) {
		return connectorapi.FileUploadStatus{}, connectors.ErrUploadStageExpired
	}
	if !component.runner.TempPathAllowed(runtime, item.TempPath) {
		return connectorapi.FileUploadStatus{}, errors.New("upload staging file is unavailable")
	}
	execution, err := handlers.resolveTransferExecution(ctx, runtime, item.RuntimeID)
	if err != nil {
		return connectorapi.FileUploadStatus{}, err
	}
	if !execution.authorizedBy(request.Authorization) {
		return connectorapi.FileUploadStatus{}, errTransferExecutionStale
	}
	bound := execution.runnerExecution()
	bound.AuthorizeUpload = authorize
	bound.UploadTokenID = request.Principal.TokenID
	if err := component.runner.LaunchUpload(ctx, runtime, item.ID, request.Manifest.Overwrite, bound); err != nil {
		return connectorapi.FileUploadStatus{}, err
	}
	return uploadStatus(item), nil
}

func (component *Component) MCPUploadStatus(ctx context.Context, workspace Workspace, reference connectorapi.FileUploadReference) (connectorapi.FileUploadStatus, error) {
	if !validUploadPrincipal(workspace, reference.Principal) {
		return connectorapi.FileUploadStatus{}, filetransfer.ErrNotFound
	}
	runtime, err := component.runtimes.RuntimeForWorkspace(workspace)
	if err != nil {
		return connectorapi.FileUploadStatus{}, err
	}
	item, requestID, err := runtime.Storage().MCPUploadStatus(ctx, reference.Principal.TokenID, reference.Ref, reference.TargetRef, reference.UploadAction)
	status := uploadStatus(item)
	status.ActionRequestID = requestID
	return status, err
}

type WorkspaceUploads struct {
	component *Component
	workspace Workspace
	handlers  *FileTransferHTTPHandlers
}

func (component *Component) UploadGateway(workspace Workspace, handlers *FileTransferHTTPHandlers) WorkspaceUploads {
	return WorkspaceUploads{component: component, workspace: workspace, handlers: handlers}
}

func (uploads WorkspaceUploads) ConnectorLaunchFileUpload(ctx context.Context, request connectorapi.FileUploadDispatch) (connectorapi.FileUploadStatus, error) {
	if uploads.component == nil || uploads.handlers == nil {
		return connectorapi.FileUploadStatus{}, filetransfer.ErrNotFound
	}
	return uploads.component.LaunchMCPUpload(ctx, uploads.workspace, uploads.handlers, request)
}

func (uploads WorkspaceUploads) ConnectorFileUploadStatus(ctx context.Context, ref connectorapi.FileUploadReference) (connectorapi.FileUploadStatus, error) {
	if uploads.component == nil {
		return connectorapi.FileUploadStatus{}, filetransfer.ErrNotFound
	}
	return uploads.component.MCPUploadStatus(ctx, uploads.workspace, ref)
}

func (uploads WorkspaceUploads) ApprovalReady(ctx context.Context, approval connectors.FileUploadApproval) (bool, error) {
	if uploads.component == nil {
		return false, nil
	}
	runtime, err := uploads.component.runtimes.RuntimeForWorkspace(uploads.workspace)
	if err != nil {
		return false, err
	}
	item, requestID, err := runtime.Storage().MCPUploadStatus(ctx, approval.TokenID, approval.Ref, approval.TargetRef, approval.ActionName)
	if errors.Is(err, filetransfer.ErrNotFound) {
		return false, nil
	}
	return requestID == approval.RequestID && filetransfer.MCPUploadReady(item, time.Now()), err
}

func validUploadPrincipal(workspace Workspace, principal connectorapi.Principal) bool {
	return principal.Validate() == nil && principal.Kind == connectorapi.PrincipalMCPToken &&
		principal.WorkspaceID == workspace.WorkspaceID && principal.RuntimeInstanceID == workspace.RuntimeID
}

func uploadStatus(item filetransfer.Record) connectorapi.FileUploadStatus {
	return connectorapi.FileUploadStatus{Status: item.Status, TransferID: item.ID, TransferredBytes: item.TransferredBytes, FailureKind: item.FailureKind}
}
