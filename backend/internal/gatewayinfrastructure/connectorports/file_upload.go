package connectorports

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func (gateway RuntimeActionGateway) ConnectorLaunchFileUpload(ctx context.Context, request connectorapi.FileUploadDispatch) (connectorapi.FileUploadStatus, error) {
	if gateway.workspace.Transfers.Uploads == nil || request.Authorization.ConnectorKind != gateway.kind {
		return connectorapi.FileUploadStatus{}, ErrRuntimeUnavailable
	}
	if err := connectortransport.RequireRuntimeID(ctx, gateway.workspace.runtime, gateway.kind, request.RuntimeID); err != nil {
		return connectorapi.FileUploadStatus{}, err
	}
	return gateway.workspace.Transfers.Uploads.ConnectorLaunchFileUpload(ctx, request)
}

func (gateway RuntimeActionGateway) ConnectorFileUploadStatus(ctx context.Context, reference connectorapi.FileUploadReference) (connectorapi.FileUploadStatus, error) {
	kind, _, _, valid := connectors.ParseTargetRef(reference.TargetRef)
	if gateway.workspace.Transfers.Uploads == nil || !valid || kind != gateway.kind {
		return connectorapi.FileUploadStatus{}, ErrRuntimeUnavailable
	}
	return gateway.workspace.Transfers.Uploads.ConnectorFileUploadStatus(ctx, reference)
}

var _ connectorapi.FileUploadGateway = RuntimeActionGateway{}
