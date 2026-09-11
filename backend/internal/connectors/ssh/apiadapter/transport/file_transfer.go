package transport

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter/management"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
)

func (Transport) BrowseRemoteFiles(ctx context.Context, server connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, remotePath string) ([]connectorapi.RemoteFileEntry, error) {
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
	return management.RemoteFileEntries(entries), nil
}

func (Transport) StatRemotePath(ctx context.Context, server connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, remotePath string) (connectorapi.RemotePathStatus, error) {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	status, err := execution.StatRemotePath(ctx, management.ExecutionTarget(gateway, target, privateKey), remotePath)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	return connectorapi.RemotePathStatus{Exists: status.Exists, Type: status.Type, Size: status.Size}, nil
}

func (Transport) UploadFile(ctx context.Context, server connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, localPath string, remotePath string, overwrite bool, options connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	result, err := execution.UploadFileWithOptions(ctx, management.ExecutionTarget(gateway, target, privateKey), localPath, remotePath, overwrite, management.ExecutionTransferOptions(options))
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	return management.ConnectorTransferResult(result), nil
}

func (Transport) DownloadFile(ctx context.Context, server connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, remotePath string, localPath string, options connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	result, err := execution.DownloadFileWithOptions(ctx, management.ExecutionTarget(gateway, target, privateKey), remotePath, localPath, management.ExecutionTransferOptions(options))
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	return management.ConnectorTransferResult(result), nil
}
