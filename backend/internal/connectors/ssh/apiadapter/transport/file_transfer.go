package transport

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter/management"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func (Transport) BrowseRemoteFiles(ctx context.Context, server connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, remotePath string) ([]connectors.RemoteFileEntry, error) {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return nil, err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return nil, err
	}
	entries, err := execution.ListRemoteDirectory(ctx, management.ExecutionTarget(gateway, target, privateKey), remotePath)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		return []connectors.RemoteFileEntry{}, nil
	}
	return entries, nil
}

func (Transport) StatRemotePath(ctx context.Context, server connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, remotePath string) (connectors.RemotePathStatus, error) {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return connectors.RemotePathStatus{}, err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return connectors.RemotePathStatus{}, err
	}
	status, err := execution.StatRemotePath(ctx, management.ExecutionTarget(gateway, target, privateKey), remotePath)
	if err != nil {
		return connectors.RemotePathStatus{}, err
	}
	return status, nil
}

func (Transport) UploadFile(ctx context.Context, server connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, localPath string, remotePath string, overwrite bool, options connectors.TransferOptions) (connectors.TransferResult, error) {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return connectors.TransferResult{}, err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return connectors.TransferResult{}, err
	}
	return execution.UploadFileWithOptions(ctx, management.ExecutionTarget(gateway, target, privateKey), localPath, remotePath, overwrite, options)
}

func (Transport) DownloadFile(ctx context.Context, server connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, remotePath string, localPath string, options connectors.TransferOptions) (connectors.TransferResult, error) {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return connectors.TransferResult{}, err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return connectors.TransferResult{}, err
	}
	return execution.DownloadFileWithOptions(ctx, management.ExecutionTarget(gateway, target, privateKey), remotePath, localPath, options)
}

func (Transport) CleanupRemoteStaging(ctx context.Context, server connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, stagingRef string) error {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return err
	}
	return execution.CleanupRemoteUploadStaging(ctx, management.ExecutionTarget(gateway, target, privateKey), stagingRef)
}

var _ connectorapi.RemoteStagingRecoveryAdapter = Transport{}
